package gates

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// errNoDecider is returned when the decider carries no user id.
var errNoDecider = errors.New("gates: decider has no user id")

// CheckDecision checks that by may decide g at now. The decider must be a human (an agent
// never approves), not in the preparer set (an agent preparer counts as the user it acted
// for), hold the kind's approver capability, and decide after the kind's cooling-off
// period. The sole-practitioner exception is SelfApproval's, never this check's.
//
// rule: BIL-I-021
func (c *Catalog) CheckDecision(ctx context.Context, g Gate, by Principal, now time.Time) error {
	k, err := c.Kind(ctx, g.Code)
	if err != nil {
		return err
	}
	if by.UserID == "" && !by.Agent {
		return fmt.Errorf("gate %s: %w", g.ID, errNoDecider)
	}
	// rule: ARC-I-016
	// The shared separation-of-duties check, with no sole-practitioner evidence: here the
	// preparer is always refused, and SelfApproval decides the sole exception.
	if _, err := CheckSoD(Duty{What: g.Code, ObjectID: g.ID, Preparer: preparerUser(g.Preparer),
		Approver: by.UserID, ApproverIsAgent: by.Agent, Now: now}); err != nil {
		return fmt.Errorf("gate %s: %w", g.ID, err)
	}
	if !slices.Contains(by.Capabilities, k.Approver) {
		return fmt.Errorf("gate %s needs %s: %w", g.ID, k.Approver, ErrMissingCapability)
	}
	if end := g.OpenedAt.Add(k.CoolingOff); now.Before(end) {
		return fmt.Errorf("gate %s until %s: %w", g.ID, end.Format(time.RFC3339), ErrCoolingOff)
	}
	return nil
}
