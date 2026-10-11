package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// ErrInvalidLevel means a level name is not one the app set with LevelOrder.
var ErrInvalidLevel = errors.New("identity: invalid level")

// RoleGrants is the optional part of a RoleAssignments that reads a role the app did not
// register (a custom role) as capability -> level name. A role it does not know returns no
// grants, so every check on it fails closed. PGAssignments reads core.role_grant.
type RoleGrants interface {
	GrantsOf(ctx context.Context, role string) (map[string]string, error)
}

// LevelOrder sets the capability level names, weakest first. The first name is no access;
// every later one holds the capability, and a check for a level passes on it or any later.
func (r *Roles) LevelOrder(names ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rank = make(map[string]int, len(names))
	for i, n := range names {
		r.rank[n] = i
	}
	r.top = len(names) - 1
}

// DecideLevel names the least level that may decide a gate on an approver capability.
func (r *Roles) DecideLevel(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.rank[name]
	if !ok || n == 0 {
		return fmt.Errorf("decide level %q: %w", name, ErrInvalidLevel)
	}
	r.decide = n
	return nil
}

// Grant records role's level on capability, registering the role if it is new. A level
// above the first also lists the capability on the role.
func (r *Roles) Grant(role, capability, level string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.rank[level]
	if !ok {
		return fmt.Errorf("grant %s/%s %q: %w", role, capability, level, ErrInvalidLevel)
	}
	if r.levels[role] == nil {
		r.levels[role] = map[string]int{}
	}
	r.levels[role][capability] = n
	if _, ok := r.roles[role]; !ok {
		r.roles[role] = nil
	}
	if n > 0 && !slices.Contains(r.roles[role], capability) {
		r.roles[role] = append(r.roles[role], capability)
	}
	return nil
}

// need is the rank a check for level needs. The empty level is the weakest holding level.
// Without LevelOrder every holding counts as one level.
func (r *Roles) need(level string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if level == "" || len(r.rank) == 0 {
		return 1, nil
	}
	n, ok := r.rank[level]
	if !ok {
		return 0, fmt.Errorf("level %q: %w", level, ErrInvalidLevel)
	}
	return n, nil
}

// deciding reports whether a check for rank on capability is a gate decision: the
// capability is an approver capability and rank reaches the decide level. Without levels,
// any check on an approver capability is.
func (r *Roles) deciding(capability string, rank int) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.approvers[capability] {
		return false
	}
	return r.decide == 0 || rank >= r.decide
}

// registered returns role's rank on capability when the app registered the role. A role
// registered without levels holds each listed capability at the top level.
func (r *Roles) registered(role, capability string) (int, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if lv, ok := r.levels[role]; ok {
		return lv[capability], true
	}
	caps, ok := r.roles[role]
	if !ok {
		return 0, false
	}
	if slices.Contains(caps, capability) {
		return max(r.top, 1), true
	}
	return 0, true
}

// customRank is a custom role's rank on capability from its grants, never above the
// strongest registered role's rank on it. An unknown level counts as no access.
func (r *Roles) customRank(grants map[string]string, capability string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := r.rank[grants[capability]]
	strongest := 0
	for _, lv := range r.levels {
		strongest = max(strongest, lv[capability])
	}
	return min(n, strongest)
}

// rolesRank is the highest rank any of roles holds on capability. A check that decides a
// gate skips reserved roles (admin, agent): they never decide. A role the app did not
// register is read through grants when set; otherwise it holds nothing (fail closed).
func (r *Roles) rolesRank(ctx context.Context, roles []string, capability string, deciding bool, grants RoleGrants) (int, error) {
	best := 0
	for _, role := range roles {
		// rule: ARC-I-018
		if deciding && reservedRoles[role] {
			continue
		}
		if n, ok := r.registered(role, capability); ok {
			best = max(best, n)
			continue
		}
		if grants == nil {
			continue
		}
		g, err := grants.GrantsOf(ctx, role)
		if err != nil {
			return 0, fmt.Errorf("grants of role: %w", err)
		}
		best = max(best, r.customRank(g, capability))
	}
	return best, nil
}
