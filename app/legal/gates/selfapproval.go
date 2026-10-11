package gates

import (
	"context"
	"fmt"
	"slices"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/registry"
)

// SelfApproval applies the sole-practitioner exception of the Counsel gate engine. It is
// default deny: the exception holds only for a human decider with the kind's approver
// capability, when the firm has exactly one active licensee and the self-approval setting
// is on. When the decider is in the preparer set, Exception is true and the caller records
// self_approval_exception with the gate id. A kind with a sole_owner_delay (GA9, GA11) may
// be decided by the sole approver only once the delay since the gate opened has passed, and
// an irreversible kind is never self-approved. The time comes from the clock in ctx.
//
// rule: ARC-I-021
func (c *Catalog) SelfApproval(ctx context.Context, in SelfApprovalInput) (SelfApprovalResult, error) {
	k, err := c.Spec(in.Gate.Code)
	if err != nil {
		return SelfApprovalResult{}, err
	}
	switch {
	case in.By.Agent:
		return SelfApprovalResult{Reason: "an agent never approves"}, nil
	case in.By.UserID == "":
		return SelfApprovalResult{Reason: "decider has no user"}, nil
	case !in.SettingEnabled:
		return SelfApprovalResult{Reason: "sole-practitioner setting is off"}, nil
	case in.ActiveLicensees != 1:
		return SelfApprovalResult{Reason: "firm is not a sole practice"}, nil
	case !slices.Contains(in.By.Capabilities, k.Approver):
		return SelfApprovalResult{Reason: "decider lacks " + k.Approver}, nil
	}
	p := in.Gate.Preparer
	self := in.By.UserID == p.UserID || (p.Agent && in.By.UserID == p.ActingFor)
	// rule: ARC-I-021
	if self && k.Risk == registry.RiskIrreversible {
		return SelfApprovalResult{Reason: "irreversible kind is never self-approved"}, nil
	}
	if k.SoleOwnerDelay > 0 {
		clk, err := clock.From(ctx)
		if err != nil {
			return SelfApprovalResult{}, fmt.Errorf("self approval: %w", err)
		}
		// Q-002: a sole eligible approver decides alone only after sole_owner_delay.
		if in.Gate.OpenedAt.IsZero() || clk.Now().Before(in.Gate.OpenedAt.Add(k.SoleOwnerDelay)) {
			return SelfApprovalResult{Reason: "sole-owner delay"}, nil
		}
	}
	return SelfApprovalResult{Allowed: true, Reason: "sole practitioner", Exception: self}, nil
}
