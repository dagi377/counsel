package gates

import (
	"context"
	"slices"
)

// SelfApproval applies the sole-practitioner exception of the Counsel gate engine. It is
// default deny: the exception holds only for a human decider with the kind's approver
// capability, when the firm has exactly one active licensee and the self-approval setting
// is on. When the decider is in the preparer set, Exception is true and the caller records
// self_approval_exception with the gate id.
//
// rule: ARC-I-021
func (c *Catalog) SelfApproval(ctx context.Context, in SelfApprovalInput) (SelfApprovalResult, error) {
	k, err := c.Kind(ctx, in.Gate.Code)
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
	return SelfApprovalResult{Allowed: true, Reason: "sole practitioner", Exception: self}, nil
}
