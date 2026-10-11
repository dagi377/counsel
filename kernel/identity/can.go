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

// userRank is the highest rank u's roles hold on capability. A role the app did not
// register is read through the assignments' RoleGrants, when they have it.
func (c *Checker) userRank(ctx context.Context, u UserID, capability string, deciding bool) (int, error) {
	if u == "" {
		return 0, ErrNoPrincipal
	}
	rs, err := c.assign.RolesOf(ctx, u)
	if err != nil {
		return 0, fmt.Errorf("roles of user: %w", err)
	}
	grants, _ := c.assign.(RoleGrants)
	return c.roles.rolesRank(ctx, rs, capability, deciding, grants)
}

func (c *Checker) userHolds(ctx context.Context, u UserID, capability string) (bool, error) {
	n, err := c.userRank(ctx, u, capability, false)
	return n >= 1, err
}

// Can implements Access: p holds capability at any level above none. Authorization is
// evaluated per call, never cached, so a decider's capability is re-checked at decision time.
func (c *Checker) Can(ctx context.Context, p Principal, capability string, s Subject) (bool, error) {
	return c.CanAt(ctx, p, capability, "", s)
}

// CanAt is Can with a minimum level: p passes when its role's level on capability is level
// or stronger (LevelOrder). The empty level is the weakest holding level. A check at or
// above the decide level on a gate approver capability is a decision: the agent and the
// reserved roles (admin, agent) never pass it.
func (c *Checker) CanAt(ctx context.Context, p Principal, capability, level string, s Subject) (bool, error) {
	if !c.roles.known(capability) {
		return false, fmt.Errorf("can %q: %w", capability, ErrUnknownCapability)
	}
	need, err := c.roles.need(level)
	if err != nil {
		return false, fmt.Errorf("can %q: %w", capability, err)
	}
	deciding := c.roles.deciding(capability, need)
	var holds bool
	var rank int
	switch p.Kind {
	case registry.PrincipalUser, registry.PrincipalGuest:
		rank, err = c.userRank(ctx, UserID(p.UserID), capability, deciding)
		holds = rank >= need
	case registry.PrincipalAgent:
		// rule: ARC-I-036
		holds = slices.Contains(p.Grant, capability)
		if holds {
			rank, err = c.userRank(ctx, UserID(p.ActingFor), capability, deciding)
			holds = rank >= need
		}
		// rule: ARC-I-018
		// The agent never decides a gate.
		if deciding {
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
