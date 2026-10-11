// CONTRACT: kernel/server types and interfaces. Frozen once on main; changes are additive only
// and need a docs/QUESTIONS.md entry. Source: spec/eng/k1-kernel.html, api/openapi.yaml.

package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/api"
)

// Route is one registered method and chi pattern, e.g. {GET /v1/work/items/{id}}.
type Route struct {
	Method  string
	Pattern string
}

// Mounter adds routes to the router. Kernel modules mount from their own files; apps mount
// only through Registry.Routes.
type Mounter func(r chi.Router)

// Server is the HTTP server the kernel builds at boot.
type Server interface {
	// Mount adds a module's routes. A route registered twice makes boot fail.
	Mount(m Mounter) error
	// Routes lists every registered route, sorted by pattern then method.
	Routes() []Route
	// Handler is the root handler with middleware applied.
	Handler() http.Handler
}

// ProblemWriter writes problem+json error responses.
type ProblemWriter interface {
	// WriteProblem writes p with Content-Type application/problem+json and p.Status.
	WriteProblem(w http.ResponseWriter, r *http.Request, p api.Problem)
}

// ErrDuplicateRoute is returned by Mount when a method and pattern are already registered.
var ErrDuplicateRoute = errors.New("server: duplicate route")

// ErrPreconditionRequired means a write arrived without If-Match (HTTP 428).
var ErrPreconditionRequired = errors.New("server: If-Match required")

// ErrPreconditionFailed means If-Match did not match the current ETag (HTTP 412).
var ErrPreconditionFailed = errors.New("server: If-Match does not match")
