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

	mu        sync.Mutex
	runs      []Run
	decisions []decision
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

// Decide implements Clearer: gate G2 on an open run.
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
	// The agent never approves; its acting-for user is a preparer, not an approver.
	switch {
	case approver.IsAgent:
		return run, fmt.Errorf("conflict decide %s: %w", runID, ErrAgentApproves)
	case approver.UserID == "" || approver.UserID == run.PreparedBy:
		return run, fmt.Errorf("conflict decide %s: %w", runID, ErrPreparerApproves)
	case !approver.IsLawyer:
		return run, fmt.Errorf("conflict decide %s: %w", runID, ErrNotLawyer)
	case run.Status != StatusOpen:
		return run, fmt.Errorf("conflict decide %s: %w", runID, ErrDecided)
	}
	c.decisions = append(c.decisions, decision{runID: runID, by: approver.UserID, approve: approve, reason: reason, at: c.now()})
	return c.view(c.runs[i]), nil
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
