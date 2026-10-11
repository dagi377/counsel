// Package rolesfake is an in-memory app/legal/roles for tests: no network, no Postgres.
package rolesfake

import (
	"context"
	"fmt"
	"sync"

	"github.com/dagi377/counsel/app/legal/roles"
)

var _ roles.Service = (*Fake)(nil)

// Fake holds an access matrix and walls in memory. The zero value is not usable; use New.
type Fake struct {
	mu     sync.Mutex
	known  map[roles.RoleID]bool
	caps   map[string]bool
	grants map[roles.RoleID]map[string]roles.Level
	walls  map[[2]string]bool // (userID, matterID)
}

// New returns a fake that knows the built-in roles and the given grants.
func New(grants ...roles.Grant) *Fake {
	f := &Fake{
		known:  map[roles.RoleID]bool{},
		caps:   map[string]bool{},
		grants: map[roles.RoleID]map[string]roles.Level{},
		walls:  map[[2]string]bool{},
	}
	for _, r := range []roles.RoleID{roles.Owner, roles.Admin, roles.Lawyer, roles.Associate,
		roles.Paralegal, roles.Clerk, roles.Bookkeeper, roles.Auditor, roles.Client, roles.Agent} {
		f.known[r] = true
	}
	for _, g := range grants {
		if err := f.SetGrant(g); err != nil {
			panic(err)
		}
	}
	return f
}

// SetGrant sets one matrix cell and registers its capability.
func (f *Fake) SetGrant(g roles.Grant) error {
	if !g.Level.Valid() {
		return fmt.Errorf("set grant %s/%s: %w", g.Role, g.Capability, roles.ErrInvalidLevel)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.known[g.Role] = true
	f.caps[g.Capability] = true
	if f.grants[g.Role] == nil {
		f.grants[g.Role] = map[string]roles.Level{}
	}
	f.grants[g.Role][g.Capability] = g.Level
	return nil
}

// Wall screens userID from matterID.
func (f *Fake) Wall(userID, matterID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.walls[[2]string{userID, matterID}] = true
}

// Level implements roles.Matrix.
func (f *Fake) Level(role roles.RoleID, capability string) (roles.Level, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.known[role] {
		return roles.None, fmt.Errorf("level %s: %w", role, roles.ErrUnknownRole)
	}
	if !f.caps[capability] {
		return roles.None, fmt.Errorf("level %s: %w", capability, roles.ErrUnknownCapability)
	}
	if l, ok := f.grants[role][capability]; ok {
		return l, nil
	}
	return roles.None, nil
}

// Can implements roles.Matrix.
func (f *Fake) Can(role roles.RoleID, capability string, need roles.Level) (bool, error) {
	if !need.Valid() {
		return false, fmt.Errorf("can %s: %w", need, roles.ErrInvalidLevel)
	}
	l, err := f.Level(role, capability)
	if err != nil {
		return false, err
	}
	return l.Covers(need), nil
}

// AgentLevel implements roles.AgentLimits.
func (f *Fake) AgentLevel(actingFor roles.RoleID, capability string) (roles.Level, error) {
	if actingFor == roles.Agent {
		return roles.None, fmt.Errorf("agent level: acting for an agent: %w", roles.ErrUnknownRole)
	}
	user, err := f.Level(actingFor, capability)
	if err != nil {
		return roles.None, err
	}
	agent, err := f.Level(roles.Agent, capability)
	if err != nil {
		return roles.None, err
	}
	// The agent never approves; at most it works on what the user may see.
	return user.Min(agent).Min(roles.All), nil
}

// AgentCan implements roles.AgentLimits.
func (f *Fake) AgentCan(actingFor roles.RoleID, capability string, need roles.Level) (bool, error) {
	if !need.Valid() {
		return false, fmt.Errorf("agent can %s: %w", need, roles.ErrInvalidLevel)
	}
	l, err := f.AgentLevel(actingFor, capability)
	if err != nil {
		return false, err
	}
	return l.Covers(need), nil
}

// Visible implements roles.Walls.
func (f *Fake) Visible(ctx context.Context, userID, matterID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("visible: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.walls[[2]string{userID, matterID}], nil
}
