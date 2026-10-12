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

	// records keeps hit dispositions, consents and G2 decisions.
	records DecisionStore
}

// NewChecks returns a Checks that searches with s and stamps times from now. Runs,
// dispositions, consents and decisions stay in this process's memory: for tests only.
func NewChecks(s Searcher, now func() time.Time) *Checks {
	return &Checks{search: s, runs: &memRuns{}, records: newMemDecisions(), now: now}
}

// NewChecksWith returns a Checks that keeps its runs in store; dispositions, consents and
// decisions stay in this process's memory. Production uses NewChecksStored.
func NewChecksWith(s Searcher, store RunStore, now func() time.Time) *Checks {
	return &Checks{search: s, runs: store, records: newMemDecisions(), now: now}
}

// NewChecksStored returns a Checks that keeps its runs in store and the conflict record of
// each run (dispositions, consents, G2 decisions) in records: PGRuns and PGDecisions in
// production, so a clearance survives a restart and every instance sees it (RPC 3.4).
func NewChecksStored(s Searcher, store RunStore, records DecisionStore, now func() time.Time) *Checks {
	return &Checks{search: s, runs: store, records: records, now: now}
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
	return c.view(ctx, run)
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
	out := make([]Run, 0, len(runs))
	for _, r := range runs {
		v, err := c.view(ctx, r)
		if err != nil {
			return nil, fmt.Errorf("conflict runs: %w", err)
		}
		out = append(out, v)
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
	run, err := c.get(ctx, runID)
	if err != nil {
		return fmt.Errorf("conflict dispose: %w", err)
	}
	if err := approverErr(run, lawyer); err != nil {
		return fmt.Errorf("conflict dispose %s: %w", runID, err)
	}
	switch d {
	case DispositionNotAConflict, DispositionNeedsConsent, DispositionNeedsWall, DispositionConflict:
	default:
		return fmt.Errorf("conflict dispose %s: %w", runID, ErrBadDisposition)
	}
	if hit < 0 || hit >= len(run.Hits) {
		return fmt.Errorf("conflict dispose %s hit %d: %w", runID, hit, ErrBadDisposition)
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return fmt.Errorf("conflict dispose %s hit %d: %w", runID, hit, ErrDispositionNote)
	}
	// rule: ARC-I-041 (LSO RPC 3.4 conflict record)
	// The disposition is part of the conflict record: it is stored, not kept in memory.
	before, after, err := c.records.Dispose(ctx, DispositionRecord{RunID: runID, Hit: hit, Disposition: d, Note: note,
		By: lawyer.UserID, At: c.now()})
	if err != nil {
		return fmt.Errorf("conflict dispose %s hit %d: %w", runID, hit, err)
	}
	// The hits-disposed event fires once: when this disposition disposed the last hit.
	if before >= len(run.Hits) || after != len(run.Hits) || c.Events == nil {
		return nil
	}
	if err := c.Events.Emit(ctx, nil, outbox.Subject{Kind: SubjectRun, ID: runID}, EventHitsDisposed, hitsDisposedOf(run)); err != nil {
		return fmt.Errorf("conflict dispose %s: event: %w", runID, err)
	}
	return nil
}

// RecordConsent appends a client's consent for one hit of an open run: requested, signed,
// refused or withdrawn. A signed consent names the signed document. Only a human lawyer
// records it; the agent never does.
func (c *Checks) RecordConsent(ctx context.Context, runID string, hit int, lawyer Actor, k Consent) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("conflict consent: %w", err)
	}
	run, err := c.get(ctx, runID)
	if err != nil {
		return fmt.Errorf("conflict consent: %w", err)
	}
	switch {
	case lawyer.IsAgent:
		return fmt.Errorf("conflict consent %s: %w", runID, ErrAgentApproves)
	case lawyer.UserID == "" || !lawyer.IsLawyer:
		return fmt.Errorf("conflict consent %s: %w", runID, ErrNotLawyer)
	case run.Status != StatusOpen:
		return fmt.Errorf("conflict consent %s: %w", runID, ErrDecided)
	case hit < 0 || hit >= len(run.Hits):
		return fmt.Errorf("conflict consent %s hit %d: %w", runID, hit, ErrBadConsent)
	}
	k.RunID, k.Hit, k.RecordedBy, k.RecordedAt = runID, hit, lawyer.UserID, c.now()
	if err := validConsent(k); err != nil {
		return fmt.Errorf("conflict consent %s hit %d: %w", runID, hit, err)
	}
	if err := c.records.RecordConsent(ctx, k); err != nil {
		return fmt.Errorf("conflict consent %s hit %d: %w", runID, hit, err)
	}
	return nil
}

// Consents returns the latest consent per hit of run runID.
func (c *Checks) Consents(ctx context.Context, runID string) (map[int]Consent, error) {
	out, err := c.records.Consents(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("conflict consents %s: %w", runID, err)
	}
	return out, nil
}

// decide records a G2 decision on an open run. Only Clearance calls it, after the gate
// check. Clearing needs every hit disposed as not a conflict, waived by signed consent, or
// walled off with signed consent, and a reason when the run has any hit. Rejecting needs
// neither. The rules are checked on the stored records in the transaction that stores the
// decision.
func (c *Checks) decide(ctx context.Context, runID string, approver Actor, approve bool, reason string) (Run, error) {
	if err := ctx.Err(); err != nil {
		return Run{}, fmt.Errorf("conflict decide: %w", err)
	}
	stored, err := c.runs.Get(ctx, runID)
	if err != nil {
		return Run{}, fmt.Errorf("conflict decide: %w", err)
	}
	run, err := c.view(ctx, stored)
	if err != nil {
		return Run{}, fmt.Errorf("conflict decide: %w", err)
	}
	if err := approverErr(run, approver); err != nil {
		return run, fmt.Errorf("conflict decide %s: %w", runID, err)
	}
	var check ClearCheck
	if approve && len(run.Hits) > 0 {
		if strings.TrimSpace(reason) == "" {
			return run, fmt.Errorf("conflict decide %s: %w", runID, ErrDispositionNote)
		}
		check = clearCheck(runID, len(run.Hits))
	}
	// rule: ARC-I-041 (LSO RPC 3.4 conflict record)
	if err := c.records.Decide(ctx, DecisionRecord{RunID: runID, By: approver.UserID, Approve: approve, Reason: reason,
		At: c.now()}, check); err != nil {
		if errors.Is(err, ErrUndisposedHits) || errors.Is(err, ErrConsentMissing) {
			return run, err // clearCheck names the run and hit
		}
		return run, fmt.Errorf("conflict decide %s: %w", runID, err)
	}
	return c.view(ctx, stored)
}

// get returns the current view of run runID from the store.
func (c *Checks) get(ctx context.Context, runID string) (Run, error) {
	r, err := c.runs.Get(ctx, runID)
	if err != nil {
		return Run{}, err
	}
	return c.view(ctx, r)
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

// view returns a copy of r with its stored decision applied. Callers cannot reach the
// stored slices.
func (c *Checks) view(ctx context.Context, r Run) (Run, error) {
	r.Terms = append([]string(nil), r.Terms...)
	r.Hits = append([]Hit(nil), r.Hits...)
	d, ok, err := c.records.Decision(ctx, r.ID)
	if err != nil {
		return Run{}, fmt.Errorf("decision of run %s: %w", r.ID, err)
	}
	if ok {
		r.Status = StatusRejected
		if d.Approve {
			r.Status = StatusCleared
		}
		r.DecidedBy, r.Reason, r.DecidedAt = d.By, d.Reason, d.At
	}
	return r, nil
}

// preparerOf is the user who counts as preparer: for an agent, the user it acts for.
func preparerOf(a Actor) string {
	if a.IsAgent {
		return a.ActingFor
	}
	return a.UserID
}
