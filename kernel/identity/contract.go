// CONTRACT: kernel/identity (T-006). Frozen once on main; changes are additive only and need
// a docs/QUESTIONS.md entry. Source: spec/eng/k1-kernel.html ("kernel/identity": Can,
// SubjectResolver, AgentPrincipal). Bodies live in the T-006 subtasks (can.go, roles.go,
// resolver.go, agent.go, session.go).

package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

// Principal, Subject and SubjectResolver are the registry types; identity does not copy them.
type (
	Principal       = registry.Principal
	PrincipalKind   = registry.PrincipalKind
	Subject         = registry.Subject
	SubjectResolver = registry.SubjectResolver
)

// UserID identifies a user (users.id).
type UserID string

// RunID identifies one agent run.
type RunID string

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrForbidden means the principal lacks the capability (problem+json 403).
	ErrForbidden = errors.New("identity: forbidden")
	// ErrNotVisible means the subject's resolver hides it from the principal. It maps to 404,
	// never 403, so a hidden subject's existence does not leak.
	ErrNotVisible = errors.New("identity: subject not visible")
	// ErrUnknownCapability means the capability id was never registered.
	ErrUnknownCapability = errors.New("identity: unknown capability")
	// ErrUnknownSubjectKind means no resolver is registered for the subject's kind.
	ErrUnknownSubjectKind = errors.New("identity: unknown subject kind")
	// ErrNoPrincipal means the request carries no authenticated principal.
	ErrNoPrincipal = errors.New("identity: no principal")
)

// Access answers who may do what. Capabilities and roles come from Registry.Capability and
// Registry.Role at boot; subject visibility comes from the app's SubjectResolver.
type Access interface {
	// Can reports whether p holds capability on s. A zero Subject skips the visibility
	// check. A subject hidden by its resolver returns (false, ErrNotVisible). An agent
	// principal holds only the intersection of its acting user's capabilities and its grant.
	Can(ctx context.Context, p Principal, capability string, s Subject) (bool, error)
	// AgentPrincipal returns the principal for an agent run acting for a user, with grant
	// narrowed to the capabilities the user holds.
	AgentPrincipal(ctx context.Context, run RunID, actingFor UserID, grant []string) (Principal, error)
}

// Session binds a principal to one database transaction (per-request RLS session vars set
// with SET LOCAL, so they vanish when tx ends).
type Session interface {
	Bind(ctx context.Context, tx pgx.Tx, p Principal) error
}
