// CONTRACT: app/legal/roles. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-006L subtasks (roles.go, agent.go,
// walls.go). Source: spec/legal/access.json (roles, matrix, sod).

package roles

import (
	"context"
	"errors"
)

// RoleID is one firm role from access.json.
type RoleID string

// Built-in roles.
const (
	Owner      RoleID = "owner"
	Admin      RoleID = "admin"
	Lawyer     RoleID = "lawyer"
	Associate  RoleID = "associate"
	Paralegal  RoleID = "paralegal"
	Clerk      RoleID = "clerk"
	Bookkeeper RoleID = "bookkeeper"
	Auditor    RoleID = "auditor"
	Client     RoleID = "client"
	Agent      RoleID = "agent"
)

// Level is how much of a capability a role holds (access.json matrix.levels).
type Level string

// Levels, weakest first.
const (
	None    Level = "none"
	View    Level = "view"
	Own     Level = "own"
	All     Level = "all"
	Approve Level = "approve"
)

var levelRank = map[Level]int{None: 0, View: 1, Own: 2, All: 3, Approve: 4}

// Valid reports whether l is one of the five levels.
func (l Level) Valid() bool {
	_, ok := levelRank[l]
	return ok
}

// Covers reports whether holding l satisfies a need for need.
func (l Level) Covers(need Level) bool {
	return l.Valid() && need.Valid() && levelRank[l] >= levelRank[need]
}

// Min returns the weaker of l and o.
func (l Level) Min(o Level) Level {
	if levelRank[o] < levelRank[l] {
		return o
	}
	return l
}

// Grant is one cell of the access matrix.
type Grant struct {
	Role       RoleID
	Capability string // module-prefixed capability id, e.g. "billing.approve"
	Level      Level
}

// Sentinel errors.
var (
	ErrUnknownRole       = errors.New("roles: unknown role")
	ErrUnknownCapability = errors.New("roles: unknown capability")
	ErrInvalidLevel      = errors.New("roles: invalid level")
)

// Matrix answers what a role may do (T-006L.1).
type Matrix interface {
	// Level returns the role's level for a capability; a capability the role is not
	// granted is None.
	Level(role RoleID, capability string) (Level, error)
	// Can reports whether the role's level covers need.
	Can(role RoleID, capability string, need Level) (bool, error)
}

// AgentLimits narrows an agent run to the user it acts for (T-006L.2). The agent never
// approves: its effective level is the weaker of the agent role and the acting-for role,
// capped below Approve.
type AgentLimits interface {
	AgentLevel(actingFor RoleID, capability string) (Level, error)
	AgentCan(actingFor RoleID, capability string, need Level) (bool, error)
}

// Walls decides whether a user may see a matter (T-006L.3). A walled user sees the
// matter, its documents, deadlines and work items as not found.
type Walls interface {
	Visible(ctx context.Context, userID, matterID string) (bool, error)
}

// Service is everything app/legal/roles provides.
type Service interface {
	Matrix
	AgentLimits
	Walls
}
