// Package decider checks who decided an approved gate. Every gate effect that moves money or
// a regulated record re-checks the deciders in the gate tx, even though the kernel refuses
// these decisions first, so an effect fails closed on a decision it should never see.
package decider

import (
	"errors"
	"fmt"

	"github.com/dagi377/counsel/kernel/registry"
)

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrNoApprover means the gate carries no approver.
	ErrNoApprover = errors.New("decider: no approver")
	// ErrAgentApproves means an approver is not a human user.
	ErrAgentApproves = errors.New("decider: agent cannot approve")
	// ErrPreparerApproves means the preparer (or the user an agent preparer acted for) approved.
	ErrPreparerApproves = errors.New("decider: preparer cannot approve")
)

// Preparer is the user who counts as the gate's preparer: an agent's acting-for user.
func Preparer(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}

// Check refuses a gate with no approver, an agent approver, or the preparer among its
// approvers. It returns the last approver, who records the effect.
func Check(g registry.Approved) (string, error) {
	if len(g.Approvers) == 0 {
		return "", fmt.Errorf("gate %s: %w", g.GateID, ErrNoApprover)
	}
	prep := Preparer(g.Preparer)
	for _, a := range g.Approvers {
		// rule: ARC-I-019
		// The agent never approves.
		if a.Kind != registry.PrincipalUser || a.UserID == "" {
			return "", fmt.Errorf("gate %s: %w", g.GateID, ErrAgentApproves)
		}
		// rule: ARC-I-027
		// The person who prepared an item never approves it.
		if prep != "" && a.UserID == prep {
			return "", fmt.Errorf("gate %s: %w", g.GateID, ErrPreparerApproves)
		}
	}
	return g.Approvers[len(g.Approvers)-1].UserID, nil
}
