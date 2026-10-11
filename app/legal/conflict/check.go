package conflict

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Searcher finds the party records that match one search term. The party index behind it is
// owned elsewhere; Checks only reads through this interface.
type Searcher interface {
	Search(ctx context.Context, term string) ([]Hit, error)
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

var (
	_ Checker = (*Checks)(nil)
	_ Clearer = (*Checks)(nil)
)

// Checks runs conflict checks and records G2 decisions. Both logs are append-only: a check
// adds a run, a re-check adds another run, and a decision adds a decision row; nothing is
// updated in place. A run's Status, DecidedBy, Reason and DecidedAt come from its decision.
type Checks struct {
	search Searcher
	now    func() time.Time

	mu           sync.Mutex
	runs         []Run
	decisions    []decision
	dispositions []dispositionOf
}

// NewChecks returns a Checks that searches with s and stamps times from now.
func NewChecks(s Searcher, now func() time.Time) *Checks {
	return &Checks{search: s, now: now}
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
	c.mu.Lock()
	defer c.mu.Unlock()
	run := Run{
		ID:         "run-" + strconv.Itoa(len(c.runs)+1),
		MatterID:   req.MatterID,
		Terms:      terms,
		Hits:       hits,
		PreparedBy: prep,
		Status:     StatusOpen,
		RanAt:      c.now(),
	}
	c.runs = append(c.runs, run)
	return c.view(run), nil
}

// Runs implements Checker.
func (c *Checks) Runs(ctx context.Context, matterID string) ([]Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("conflict runs: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Run
	for _, r := range c.runs {
		if r.MatterID == matterID {
			out = append(out, c.view(r))
		}
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
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.find(runID)
	if i < 0 {
		return fmt.Errorf("conflict dispose %s: %w", runID, ErrNotFound)
	}
	run := c.view(c.runs[i])
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
	c.dispositions = append(c.dispositions, dispositionOf{runID: runID, hit: hit, d: d, note: note, by: lawyer.UserID, at: c.now()})
	return nil
}

// Decide implements Clearer: gate G2 on an open run. Clearing needs every hit disposed as
// not a conflict, waived by consent, or walled off with consent, and a reason when the run
// has any hit. Rejecting needs neither.
func (c *Checks) Decide(ctx context.Context, runID string, approver Actor, approve bool, reason string) (Run, error) {
	if err := ctx.Err(); err != nil {
		return Run{}, fmt.Errorf("conflict decide: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.find(runID)
	if i < 0 {
		return Run{}, fmt.Errorf("conflict decide %s: %w", runID, ErrNotFound)
	}
	run := c.view(c.runs[i])
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
	return c.view(c.runs[i]), nil
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

func (c *Checks) find(runID string) int {
	for i, r := range c.runs {
		if r.ID == runID {
			return i
		}
	}
	return -1
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
