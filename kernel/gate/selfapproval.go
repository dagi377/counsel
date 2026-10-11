package gate

import (
	"context"
)

// denyReason is the reason DenySelfApproval gives.
const denyReason = "no self-approval policy is installed"

// DenySelfApproval is the engine's self-approval hook when the app installs none: the
// preparer never approves their own gate.
func DenySelfApproval(context.Context, Gate, Principal) (bool, string) {
	return false, denyReason
}

// SelfApproval returns the engine's one self-approval hook: the policy the app installed
// with SetSelfApprovalPolicy, or DenySelfApproval when there is none. The kernel never
// installs a policy of its own.
func (e *PG) SelfApproval() SelfApprovalPolicy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.policy == nil {
		return DenySelfApproval
	}
	return e.policy
}

// MaySelfApprove asks policy whether by may approve their own gate g. A nil policy denies,
// and only a human user is ever asked about: an agent or a guest is denied whatever the
// policy says.
func MaySelfApprove(ctx context.Context, policy SelfApprovalPolicy, g Gate, by Principal) (bool, string) {
	if by.Kind != PrincipalUser || by.UserID == "" {
		return false, "only a human user may self-approve"
	}
	if policy == nil {
		return DenySelfApproval(ctx, g, by)
	}
	return policy(ctx, g, by)
}
