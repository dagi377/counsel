// Package identityfake is an in-memory identity.Access and identity.Session for tests.
package identityfake

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

var (
	_ identity.Access  = (*Fake)(nil)
	_ identity.Session = (*Fake)(nil)
)

// Fake holds capabilities, roles, user role assignments and subject resolvers in memory.
type Fake struct {
	mu        sync.RWMutex
	caps      map[string]bool
	roles     map[string][]string // role id -> capability ids
	users     map[identity.UserID][]string
	resolvers map[string]identity.SubjectResolver
	bound     []identity.Principal
}

// New returns an empty Fake.
func New() *Fake {
	return &Fake{
		caps:      map[string]bool{},
		roles:     map[string][]string{},
		users:     map[identity.UserID][]string{},
		resolvers: map[string]identity.SubjectResolver{},
	}
}

// AddCapability registers a capability id.
func (f *Fake) AddCapability(c registry.Capability) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.caps[c.ID] = true
}

// AddRole registers a role.
func (f *Fake) AddRole(r registry.Role) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.roles[r.ID] = slices.Clone(r.Capabilities)
}

// Assign gives a user a role.
func (f *Fake) Assign(u identity.UserID, role string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[u] = append(f.users[u], role)
}

// AddResolver registers the resolver for a subject kind.
func (f *Fake) AddResolver(kind string, r identity.SubjectResolver) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolvers[kind] = r
}

func (f *Fake) userHolds(u identity.UserID, capability string) bool {
	for _, role := range f.users[u] {
		if slices.Contains(f.roles[role], capability) {
			return true
		}
	}
	return false
}

// Can implements identity.Access.
func (f *Fake) Can(ctx context.Context, p identity.Principal, capability string, s identity.Subject) (bool, error) {
	f.mu.RLock()
	known := f.caps[capability]
	var holds bool
	switch p.Kind {
	case registry.PrincipalUser, registry.PrincipalGuest:
		holds = f.userHolds(identity.UserID(p.UserID), capability)
	case registry.PrincipalAgent:
		holds = slices.Contains(p.Grant, capability) && f.userHolds(identity.UserID(p.ActingFor), capability)
	}
	res, hasRes := f.resolvers[s.Kind]
	f.mu.RUnlock()

	if !known {
		return false, fmt.Errorf("can %q: %w", capability, identity.ErrUnknownCapability)
	}
	if s != (identity.Subject{}) {
		if !hasRes {
			return false, fmt.Errorf("can %q: subject kind %q: %w", capability, s.Kind, identity.ErrUnknownSubjectKind)
		}
		visible, err := res.Visible(ctx, p, s)
		if err != nil {
			return false, fmt.Errorf("can %q: resolve subject: %w", capability, err)
		}
		if !visible {
			return false, identity.ErrNotVisible
		}
	}
	return holds, nil
}

// AgentPrincipal implements identity.Access: grant is narrowed to what the user holds.
func (f *Fake) AgentPrincipal(_ context.Context, run identity.RunID, actingFor identity.UserID, grant []string) (identity.Principal, error) {
	if run == "" || actingFor == "" {
		return identity.Principal{}, fmt.Errorf("agent principal: %w", identity.ErrNoPrincipal)
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	var kept []string
	for _, c := range grant {
		if f.userHolds(actingFor, c) && !slices.Contains(kept, c) {
			kept = append(kept, c)
		}
	}
	return identity.Principal{
		Kind:      registry.PrincipalAgent,
		RunID:     string(run),
		ActingFor: string(actingFor),
		Grant:     kept,
	}, nil
}

// Bind implements identity.Session by recording p; tx is ignored.
func (f *Fake) Bind(_ context.Context, _ pgx.Tx, p identity.Principal) error {
	if p.Kind == "" {
		return identity.ErrNoPrincipal
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bound = append(f.bound, p)
	return nil
}

// Bound returns every principal passed to Bind, in order.
func (f *Fake) Bound() []identity.Principal {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return slices.Clone(f.bound)
}
