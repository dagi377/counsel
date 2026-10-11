package conflict

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/gates"
)

// GateClearance is the gate kind code of spec gate G2 (conflict clearance).
const GateClearance = "conflict.clearance"

// Capabilities returns the capabilities a user holds. Actor carries none, so Clearance looks
// them up to check the G2 approver capability.
type Capabilities interface {
	Capabilities(ctx context.Context, userID string) ([]string, error)
}

var _ Clearer = (*Clearance)(nil)

// Clearance is the only Clearer the app wires: it decides a run through gate G2, so the
// gate's capability and cooling-off rules apply on top of the lawyer and preparer rules of
// Checks.decide. It never edits a run; a decision is appended by Checks.
type Clearance struct {
	checks *Checks
	gates  gates.Gates
	caps   Capabilities
	now    func() time.Time
}

// NewClearance returns a Clearance that records decisions in c and checks them with g.
func NewClearance(c *Checks, g gates.Gates, caps Capabilities, now func() time.Time) *Clearance {
	return &Clearance{checks: c, gates: g, caps: caps, now: now}
}

// Decide implements Clearer. The approver must be a human lawyer who holds the G2 approver
// capability and is not the run's preparer (for an agent-prepared run, not the user it
// acted for), and the run must still be open.
func (c *Clearance) Decide(ctx context.Context, runID string, approver Actor, approve bool, reason string) (Run, error) {
	run, err := c.run(runID)
	if err != nil {
		return Run{}, err
	}
	if run.Status != StatusOpen {
		return run, fmt.Errorf("conflict clearance %s: %w", runID, ErrDecided)
	}
	by := gates.Principal{UserID: approver.UserID, Agent: approver.IsAgent, ActingFor: approver.ActingFor}
	if !approver.IsAgent && approver.UserID != "" {
		if by.Capabilities, err = c.caps.Capabilities(ctx, approver.UserID); err != nil {
			return run, fmt.Errorf("conflict clearance %s: capabilities: %w", runID, err)
		}
	}
	g := gates.Gate{ID: runID, Code: GateClearance, Preparer: gates.Principal{UserID: run.PreparedBy}, OpenedAt: run.RanAt}
	if err := c.gates.CheckDecision(ctx, g, by, c.now()); err != nil {
		return run, fmt.Errorf("conflict clearance %s: %w", runID, gateError(err))
	}
	return c.checks.decide(ctx, runID, approver, approve, reason)
}

// run returns the current view of one run.
func (c *Clearance) run(runID string) (Run, error) {
	c.checks.mu.Lock()
	defer c.checks.mu.Unlock()
	i := c.checks.find(runID)
	if i < 0 {
		return Run{}, fmt.Errorf("conflict clearance %s: %w", runID, ErrNotFound)
	}
	return c.checks.view(c.checks.runs[i]), nil
}

// gateError adds the conflict sentinel to a gate refusal that has one, so callers can test
// for either.
func gateError(err error) error {
	switch {
	case errors.Is(err, gates.ErrAgentApproves):
		return fmt.Errorf("%w: %w", ErrAgentApproves, err)
	case errors.Is(err, gates.ErrPreparerApproves):
		return fmt.Errorf("%w: %w", ErrPreparerApproves, err)
	}
	return err
}
