package identity

import (
	"context"
	"fmt"
	"slices"

	"github.com/dagi377/counsel/kernel/registry"
)

var _ Access = (*Checker)(nil)

// Checker is the kernel Access: capabilities from the app's registered roles, visibility from
// the app's SubjectResolver.
type Checker struct {
	roles  *Roles
	assign RoleAssignments
}

// NewChecker returns an Access over roles and user role assignments.
func NewChecker(roles *Roles, assign RoleAssignments) *Checker {
	return &Checker{roles: roles, assign: assign}
}

func (c *Checker) userHolds(ctx context.Context, u UserID, capability string) (bool, error) {
	if u == "" {
		return false, ErrNoPrincipal
	}
	rs, err := c.assign.RolesOf(ctx, u)
	if err != nil {
		return false, fmt.Errorf("roles of user: %w", err)
	}
	return c.roles.rolesHold(rs, capability), nil
}

// Can implements Access. Authorization is evaluated per call, never cached, so a decider's
// capability is re-checked at decision time.
func (c *Checker) Can(ctx context.Context, p Principal, capability string, s Subject) (bool, error) {
	if !c.roles.known(capability) {
		return false, fmt.Errorf("can %q: %w", capability, ErrUnknownCapability)
	}
	var holds bool
	var err error
	switch p.Kind {
	case registry.PrincipalUser, registry.PrincipalGuest:
		holds, err = c.userHolds(ctx, UserID(p.UserID), capability)
	case registry.PrincipalAgent:
		// rule: ARC-I-036
		holds = slices.Contains(p.Grant, capability)
		if holds {
			holds, err = c.userHolds(ctx, UserID(p.ActingFor), capability)
		}
		// rule: ARC-I-018
		if c.roles.IsApprover(capability) {
			holds = false
		}
	default:
		return false, fmt.Errorf("can %q: %w", capability, ErrNoPrincipal)
	}
	if err != nil {
		return false, fmt.Errorf("can %q: %w", capability, err)
	}
	if s == (Subject{}) {
		return holds, nil
	}
	// rule: ARC-I-042
	res, ok := c.roles.resolver(s.Kind)
	if !ok {
		return false, fmt.Errorf("can %q: subject kind %q: %w", capability, s.Kind, ErrUnknownSubjectKind)
	}
	visible, err := res.Visible(ctx, p, s)
	if err != nil {
		return false, fmt.Errorf("can %q: resolve subject: %w", capability, err)
	}
	if !visible {
		return false, ErrNotVisible
	}
	return holds, nil
}

// AgentPrincipal implements Access: grant is narrowed to the capabilities the user holds.
func (c *Checker) AgentPrincipal(ctx context.Context, run RunID, actingFor UserID, grant []string) (Principal, error) {
	if run == "" || actingFor == "" {
		return Principal{}, fmt.Errorf("agent principal: %w", ErrNoPrincipal)
	}
	var kept []string
	for _, g := range grant {
		// rule: ARC-I-036
		ok, err := c.userHolds(ctx, actingFor, g)
		if err != nil {
			return Principal{}, fmt.Errorf("agent principal: %w", err)
		}
		if ok && !slices.Contains(kept, g) {
			kept = append(kept, g)
		}
	}
	return Principal{Kind: registry.PrincipalAgent, RunID: string(run), ActingFor: string(actingFor), Grant: kept}, nil
}
