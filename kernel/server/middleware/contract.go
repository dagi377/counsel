// CONTRACT: kernel/server/middleware. The shape of this file is frozen after wave 0; changes
// are additive only and need a docs/QUESTIONS.md entry. Bodies live in the T-008 subtasks.

package middleware

import (
	"errors"
	"net/http"
)

// Header names.
const (
	HeaderRequestID     = "X-Request-Id"
	HeaderAuthorization = "Authorization"
)

// ProblemContentType is the media type of every error body (RFC 9457).
const ProblemContentType = "application/problem+json"

// Sentinel errors.
var (
	// ErrUnauthenticated: no credential, or one that does not verify (HTTP 401).
	ErrUnauthenticated = errors.New("middleware: unauthenticated")
	// ErrForbidden: the principal is known but may not do this (HTTP 403).
	ErrForbidden = errors.New("middleware: forbidden")
)

// Middleware wraps an http.Handler.
type Middleware func(next http.Handler) http.Handler

// PrincipalKind tells users, agents and portal guests apart.
type PrincipalKind string

// Principal kinds.
const (
	PrincipalUser  PrincipalKind = "user"
	PrincipalAgent PrincipalKind = "agent"
	PrincipalGuest PrincipalKind = "guest"
)

// Principal is who a request acts as. An agent principal carries the user it acts for.
type Principal struct {
	ID        string
	Kind      PrincipalKind
	ActingFor string
}

// Problem is a problem+json body. Rule carries the rule id an error enforces, when any.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Instance  string `json:"instance,omitempty"`
	Rule      string `json:"rule,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// Authenticator resolves the request's credential to a Principal, or returns
// ErrUnauthenticated.
type Authenticator interface {
	Authenticate(r *http.Request) (Principal, error)
}

// ProblemWriter writes p as problem+json. It never writes secrets or client data.
type ProblemWriter interface {
	WriteProblem(w http.ResponseWriter, r *http.Request, p Problem)
}

// RequestIDs mints a request id when the client sent none.
type RequestIDs interface {
	NewRequestID() string
}
