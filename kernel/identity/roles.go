package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/dagi377/counsel/kernel/registry"
)

// RoleAssignments says which roles a user holds. PGAssignments reads core.app_user.role_id;
// tests pass a map.
type RoleAssignments interface {
	RolesOf(ctx context.Context, u UserID) ([]string, error)
}

// StaticAssignments is a fixed RoleAssignments keyed by user.
type StaticAssignments map[UserID][]string

// RolesOf implements RoleAssignments.
func (s StaticAssignments) RolesOf(_ context.Context, u UserID) ([]string, error) {
	return slices.Clone(s[u]), nil
}

// reservedRoles never hold a gate approver capability by default: an admin configures the
// platform and an agent only drafts and proposes.
var reservedRoles = map[string]bool{"admin": true, "agent": true}

// Roles holds the capabilities, roles, subject resolvers and gate approver capabilities the
// app registers at boot. Register it by passing its methods through the kernel registry.
type Roles struct {
	mu        sync.RWMutex
	caps      map[string]bool
	roles     map[string][]string
	resolvers map[string]SubjectResolver
	approvers map[string]bool
	errs      []error
	// Levels (levels.go): rank by level name, weakest first; per role and capability the
	// rank held; the least rank that may decide a gate. Empty when the app sets no levels.
	rank   map[string]int
	top    int
	levels map[string]map[string]int
	decide int
}

// NewRoles returns an empty Roles.
func NewRoles() *Roles {
	return &Roles{
		caps:      map[string]bool{},
		roles:     map[string][]string{},
		resolvers: map[string]SubjectResolver{},
		approvers: map[string]bool{},
		rank:      map[string]int{},
		levels:    map[string]map[string]int{},
	}
}

// Capability registers one capability (Registry.Capability).
func (r *Roles) Capability(c registry.Capability) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c.ID == "" {
		r.errs = append(r.errs, fmt.Errorf("capability: empty id"))
		return
	}
	r.caps[c.ID] = true
}

// Role registers one role (Registry.Role).
func (r *Roles) Role(x registry.Role) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if x.ID == "" {
		r.errs = append(r.errs, fmt.Errorf("role: empty id"))
		return
	}
	r.roles[x.ID] = slices.Clone(x.Capabilities)
}

// SubjectKind registers the resolver for a subject kind (Registry.SubjectKind).
func (r *Roles) SubjectKind(k registry.SubjectKind, res SubjectResolver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolvers[k.ID] = res
}

// GateKind records the gate kind's approver capability (Registry.GateKind).
func (r *Roles) GateKind(g registry.GateKind) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g.Approver != "" {
		r.approvers[g.Approver] = true
	}
}

// Validate reports registration errors: a role granting an unregistered capability, and a
// reserved role (admin, agent) holding a gate approver capability.
func (r *Roles) Validate() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	errs := slices.Clone(r.errs)
	ids := make([]string, 0, len(r.roles))
	for id := range r.roles {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		for _, c := range r.roles[id] {
			if !r.caps[c] {
				errs = append(errs, fmt.Errorf("role %q: capability %q: %w", id, c, ErrUnknownCapability))
			}
			// rule: ARC-I-018
			// A role with levels is capped below the decide level instead (levels.go).
			if reservedRoles[id] && r.approvers[c] && r.levels[id] == nil {
				errs = append(errs, fmt.Errorf("role %q: holds gate approver capability %q: %w", id, c, ErrForbidden))
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("identity roles: %w", errors.Join(errs...))
}

// IsApprover reports whether capability is some gate kind's approver capability.
func (r *Roles) IsApprover(capability string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.approvers[capability]
}

func (r *Roles) known(capability string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.caps[capability]
}

func (r *Roles) resolver(kind string) (SubjectResolver, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res, ok := r.resolvers[kind]
	return res, ok
}
