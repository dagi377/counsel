package identity

import (
	"context"
	"fmt"
	"slices"
)

// CapabilityLookup answers which capabilities a user holds: the union of the capabilities of
// the user's assigned roles, as registered in Roles. It is generic: it knows no app role or
// capability by name. It fails closed: a user with no role assignment is ErrUnknownUser and
// an assigned role that was never registered is ErrUnknownRole, never an empty grant.
type CapabilityLookup struct {
	Roles       *Roles
	Assignments RoleAssignments
}

// Capabilities returns the sorted, de-duplicated capabilities userID holds.
func (l CapabilityLookup) Capabilities(ctx context.Context, userID string) ([]string, error) {
	if l.Roles == nil || l.Assignments == nil {
		return nil, fmt.Errorf("capabilities of %q: no roles or assignments: %w", userID, ErrForbidden)
	}
	roles, err := l.Assignments.RolesOf(ctx, UserID(userID))
	if err != nil {
		return nil, fmt.Errorf("capabilities of %q: %w", userID, err)
	}
	if len(roles) == 0 {
		return nil, fmt.Errorf("capabilities of %q: %w", userID, ErrUnknownUser)
	}
	var out []string
	for _, id := range roles {
		caps, ok := l.Roles.roleCapabilities(id)
		if !ok {
			return nil, fmt.Errorf("capabilities of %q: role %q: %w", userID, id, ErrUnknownRole)
		}
		out = append(out, caps...)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// roleCapabilities returns a copy of the capabilities role id grants, and whether id is a
// registered role.
func (r *Roles) roleCapabilities(id string) ([]string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	caps, ok := r.roles[id]
	return slices.Clone(caps), ok
}
