package conflict

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/outbox"
)

// Searcher finds the party records that match one search term. The party index behind it is
// owned elsewhere; Checks only reads through this interface.
type Searcher interface {
	Search(ctx context.Context, term string) ([]Hit, error)
}

// IndexVersioner is a Searcher that reports the party index version it searches, so a run
// records which index it saw (conflict.check_run.index_version). Checks asks for it after
// every term was searched.
type IndexVersioner interface {
	IndexVersion(ctx context.Context) (int64, error)
}

// RunStore keeps conflict runs, append-only. Append assigns the run's ID and, when Version
// is zero, the next version of its check; a second run with the same (check, version) is
// ErrRunExists and nothing is overwritten.
type RunStore interface {
	Append(ctx context.Context, r Run) (Run, error)
	// Runs lists every run for a matter, oldest first.
	Runs(ctx context.Context, matterID string) ([]Run, error)
	// Get returns run id, or ErrNotFound.
	Get(ctx context.Context, id string) (Run, error)
}

// Run store errors.
var (
	// ErrRunExists: a run with this (check, version) is already stored.
	ErrRunExists = errors.New("conflict: run version already recorded")
	// ErrInvalidRun: a run or hit is missing a value its store needs; nothing is defaulted.
	ErrInvalidRun = errors.New("conflict: run is missing required values")
)

// memRuns is the in-memory RunStore NewChecks uses.
type memRuns struct {
	mu   sync.Mutex
	runs []Run
}

func (m *memRuns) Append(ctx context.Context, r Run) (Run, error) {
	if err := ctx.Err(); err != nil {
		return Run{}, fmt.Errorf("append run: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := r.CheckID
	if key == "" {
		key = "matter:" + r.MatterID
	}
	last := 0
	for _, x := range m.runs {
		k := x.CheckID
		if k == "" {
			k = "matter:" + x.MatterID
		}
		if k != key {
			continue
		}
		if r.Version != 0 && x.Version == r.Version {
			return Run{}, fmt.Errorf("append run version %d: %w", r.Version, ErrRunExists)
		}
		if x.Version > last {
			last = x.Version
		}
	}
	if r.Version == 0 {
		r.Version = last + 1
	}
	r.ID = "run-" + strconv.Itoa(len(m.runs)+1)
	r.Terms = append([]string(nil), r.Terms...)
	r.Hits = append([]Hit(nil), r.Hits...)
	m.runs = append(m.runs, r)
	return r, nil
}

func (m *memRuns) Runs(ctx context.Context, matterID string) ([]Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("runs: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Run
	for _, r := range m.runs {
		if r.MatterID == matterID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memRuns) Get(ctx context.Context, id string) (Run, error) {
	if err := ctx.Err(); err != nil {
		return Run{}, fmt.Errorf("get run: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.ID == id {
			return r, nil
		}
	}
	return Run{}, fmt.Errorf("run %s: %w", id, ErrNotFound)
}

// errNoPreparer is returned for a check with no preparing user.
var errNoPreparer = errors.New("conflict: check has no preparer")

// Clearance errors.
var (
	// ErrUndisposedHits means G2 cannot clear: a hit has no disposition, or is a conflict.
	ErrUndisposedHits = errors.New("conflict: every hit needs a clearing disposition before G2 clears")
	// ErrDispositionNote means a disposition, or a clearance of a run with hits, has no reason.
	ErrDispositionNote = errors.New("conflict: disposition needs a reason")
	// ErrBadDisposition means an unknown disposition or a hit index outside the run.
	ErrBadDisposition = errors.New("conflict: invalid disposition")
)

// Disposition is a lawyer's finding on one hit. Values mirror the
// conflict.check_hit.disposition check constraint.
type Disposition string

// Dispositions. Only NotAConflict, NeedsConsent and NeedsWall let G2 clear; Conflict means
// the run can only be rejected.
const (
	// DispositionNotAConflict: cleared; the note says why it is not a conflict.
	DispositionNotAConflict Disposition = "not_a_conflict"
	// DispositionNeedsConsent: the conflict is waived; the note references the written consent.
	DispositionNeedsConsent Disposition = "needs_consent"
	// DispositionNeedsWall: walled off with consent; the note references the wall and consent.
	DispositionNeedsWall Disposition = "needs_wall"
	// DispositionConflict: a real conflict; G2 cannot clear.
	DispositionConflict Disposition = "conflict"
)

// dispositionOf is one disposition of one hit. Dispositions are appended; the latest per
// hit counts.
type dispositionOf struct {
	runID string
	hit   int
	d     Disposition
	note  string
	by    string
	at    time.Time
}

// decision is one G2 decision on a run. Decisions are appended, never edited.
type decision struct {
	runID   string
	by      string
	approve bool
	reason  string
	at      time.Time
}

// Checks is deliberately not a Clearer: the only Clearer is Clearance, which goes through
// gate G2. There is no gate-less clearing path.
var _ Checker = (*Checks)(nil)

// Checks runs conflict checks and records G2 decisions. Both logs are append-only: a check
// adds a run, a re-check adds another run, and a decision adds a decision row; nothing is
// updated in place. A run's Status, DecidedBy, Reason and DecidedAt come from its decision.
type Checks struct {
	search Searcher
	runs   RunStore
	now    func() time.Time
	// Events receives EventHitsDisposed when a disposition leaves every hit of a run disposed.
	// Boot sets it; while nil, no event is published and F-CON-03 does not start.
	Events outbox.Emitter

	mu           sync.Mutex
	decisions    []decision
	dispositions []dispositionOf
}

// NewChecks returns a Checks that searches with s and stamps times from now.
func NewChecks(s Searcher, now func() time.Time) *Checks {
	return &Checks{search: s, runs: &memRuns{}, now: now}
}

// NewChecksWith returns a Checks that keeps its runs in store (PGRuns in production). G2
// decisions and hit dispositions stay in memory (Q-PORT-18 C).
func NewChecksWith(s Searcher, store RunStore, now func() time.Time) *Checks {
	return &Checks{search: s, runs: store, now: now}
}

// Check implements Checker.
func (c *Checks) Check(ctx context.Context, req Request) (Run, error) {
	terms := make([]string, 0, len(req.Terms))
	for _, t := range req.Terms {
		if t = strings.TrimSpace(t); t != "" {
			terms = append(terms, t)
		}
	}
	if len(terms) == 0 {
		return Run{}, fmt.Errorf("conflict check: %w", ErrNoTerms)
	}
	prep := preparerOf(req.Preparer)
	if prep == "" {
		return Run{}, errNoPreparer
	}
	var hits []Hit
	for _, t := range terms {
		h, err := c.search.Search(ctx, t)
		if err != nil {
			// Terms are client data: the error carries the position, not the term.
			return Run{}, fmt.Errorf("conflict check: search: %w", err)
		}
		hits = append(hits, h...)
	}
	var index int64
	if v, ok := c.search.(IndexVersioner); ok {
		var err error
		if index, err = v.IndexVersion(ctx); err != nil {
			return Run{}, fmt.Errorf("conflict check: index version: %w", err)
		}
	}
	run, err := c.runs.Append(ctx, Run{
		MatterID:     req.MatterID,
		Terms:        terms,
		Hits:         hits,
		PreparedBy:   prep,
		Status:       StatusOpen,
		RanAt:        c.now(),
		CheckID:      req.CheckID,
		IndexVersion: index,
		Complete:     true,
	})
	if err != nil {
		return Run{}, fmt.Errorf("conflict check: %w", err)
	}
	if err := c.publishCompleted(ctx, run, prep); err != nil {
		return Run{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.view(run), nil
}

// publishCompleted publishes EventCheckCompleted for a stored run, acting for its preparer.
// The run store has no tx to share (Q-PORT-18 C), so the emitter opens its own.
func (c *Checks) publishCompleted(ctx context.Context, run Run, prep string) error {
	if c.Events == nil {
		return nil
	}
	id := run.CheckID
	if id == "" {
		id = run.ID
	}
	// rule: ARC-I-030
	if err := PublishCheckCompleted(ctx, c.Events, nil, CheckCompleted{
		CheckID: id, MatterID: run.MatterID, HitCount: int64(len(run.Hits)), UserID: prep,
	}); err != nil {
		return fmt.Errorf("conflict check: event: %w", err)
	}
	return nil
}

// Runs implements Checker.
func (c *Checks) Runs(ctx context.Context, matterID string) ([]Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("conflict runs: %w", err)
	}
	runs, err := c.runs.Runs(ctx, matterID)
	if err != nil {
		return nil, fmt.Errorf("conflict runs: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Run, 0, len(runs))
	for _, r := range runs {
		out = append(out, c.view(r))
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// Dispose records a lawyer's disposition of hit (its index in the run's Hits) on an open run.
// Every disposition needs a note: why it is not a conflict, or the consent and wall it relies
// on. The agent and the preparer may not dispose, for the same reason they may not clear.
func (c *Checks) Dispose(ctx context.Context, runID string, hit int, lawyer Actor, d Disposition, note string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("conflict dispose: %w", err)
	}
	stored, err := c.runs.Get(ctx, runID)
	if err != nil {
		return fmt.Errorf("conflict dispose: %w", err)
	}
	run, last, err := c.dispose(runID, stored, hit, lawyer, d, note)
	if err != nil || !last || c.Events == nil {
		return err
	}
	// Dispositions are kept in memory (Q-PORT-18 C), so there is no tx to share.
	if err := c.Events.Emit(ctx, nil, outbox.Subject{Kind: SubjectRun, ID: runID}, EventHitsDisposed, hitsDisposedOf(run)); err != nil {
		return fmt.Errorf("conflict dispose %s: event: %w", runID, err)
	}
	return nil
}

// dispose checks and appends one disposition. last is true when it disposed the run's last
// undisposed hit, so the hits-disposed event fires once; re-disposing a hit of a fully
// disposed run is not last.
func (c *Checks) dispose(runID string, stored Run, hit int, lawyer Actor, d Disposition, note string) (run Run, last bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	run = c.view(stored)
	if err := approverErr(run, lawyer); err != nil {
		return run, false, fmt.Errorf("conflict dispose %s: %w", runID, err)
	}
	switch d {
	case DispositionNotAConflict, DispositionNeedsConsent, DispositionNeedsWall, DispositionConflict:
	default:
		return run, false, fmt.Errorf("conflict dispose %s: %w", runID, ErrBadDisposition)
	}
	if hit < 0 || hit >= len(run.Hits) {
		return run, false, fmt.Errorf("conflict dispose %s hit %d: %w", runID, hit, ErrBadDisposition)
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return run, false, fmt.Errorf("conflict dispose %s hit %d: %w", runID, hit, ErrDispositionNote)
	}
	before := len(c.latestDispositions(runID))
	c.dispositions = append(c.dispositions, dispositionOf{runID: runID, hit: hit, d: d, note: note, by: lawyer.UserID, at: c.now()})
	return run, before < len(run.Hits) && len(c.latestDispositions(runID)) == len(run.Hits), nil
}

// decide records a G2 decision on an open run. Only Clearance calls it, after the gate check. Clearing needs every hit disposed as
// not a conflict, waived by consent, or walled off with consent, and a reason when the run
// has any hit. Rejecting needs neither.
func (c *Checks) decide(ctx context.Context, runID string, approver Actor, approve bool, reason string) (Run, error) {
	if err := ctx.Err(); err != nil {
		return Run{}, fmt.Errorf("conflict decide: %w", err)
	}
	stored, err := c.runs.Get(ctx, runID)
	if err != nil {
		return Run{}, fmt.Errorf("conflict decide: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	run := c.view(stored)
	if err := approverErr(run, approver); err != nil {
		return run, fmt.Errorf("conflict decide %s: %w", runID, err)
	}
	if approve && len(run.Hits) > 0 {
		latest := c.latestDispositions(runID)
		for h := range run.Hits {
			switch latest[h] {
			case DispositionNotAConflict, DispositionNeedsConsent, DispositionNeedsWall:
			default:
				return run, fmt.Errorf("conflict decide %s hit %d: %w", runID, h, ErrUndisposedHits)
			}
		}
		if strings.TrimSpace(reason) == "" {
			return run, fmt.Errorf("conflict decide %s: %w", runID, ErrDispositionNote)
		}
	}
	c.decisions = append(c.decisions, decision{runID: runID, by: approver.UserID, approve: approve, reason: reason, at: c.now()})
	return c.view(stored), nil
}

// get returns the current view of run runID from the store.
func (c *Checks) get(ctx context.Context, runID string) (Run, error) {
	r, err := c.runs.Get(ctx, runID)
	if err != nil {
		return Run{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.view(r), nil
}

// approverErr checks who may dispose or decide on run: a lawyer, not the agent and not the
// preparer, while the run is open. The agent's acting-for user is a preparer.
func approverErr(run Run, a Actor) error {
	switch {
	case a.IsAgent:
		return ErrAgentApproves
	case a.UserID == "" || a.UserID == run.PreparedBy:
		return ErrPreparerApproves
	case !a.IsLawyer:
		return ErrNotLawyer
	case run.Status != StatusOpen:
		return ErrDecided
	}
	return nil
}

// latestDispositions maps each disposed hit of runID to its latest disposition.
func (c *Checks) latestDispositions(runID string) map[int]Disposition {
	out := map[int]Disposition{}
	for _, d := range c.dispositions {
		if d.runID == runID {
			out[d.hit] = d.d
		}
	}
	return out
}

// view returns a copy of r with its decision applied. Callers cannot reach the stored slices.
func (c *Checks) view(r Run) Run {
	r.Terms = append([]string(nil), r.Terms...)
	r.Hits = append([]Hit(nil), r.Hits...)
	for _, d := range c.decisions {
		if d.runID != r.ID {
			continue
		}
		r.Status = StatusRejected
		if d.approve {
			r.Status = StatusCleared
		}
		r.DecidedBy, r.Reason, r.DecidedAt = d.by, d.reason, d.at
		break
	}
	return r
}

// preparerOf is the user who counts as preparer: for an agent, the user it acts for.
func preparerOf(a Actor) string {
	if a.IsAgent {
		return a.ActingFor
	}
	return a.UserID
}
