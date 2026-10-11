package withdraw

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// registrars holds this package's registrations. Each feature file appends to it from init()
// through add, so a feature adds a file instead of editing a shared one.
var registrars []func(registry.Registry) error

// add appends fn to the registrars Register runs. Call it only from init().
func add(fn func(registry.Registry) error) {
	registrars = append(registrars, fn)
}

// Register runs every registrar of this package, in the order the files added them.
func Register(r registry.Registry) error {
	for _, fn := range registrars {
		if err := fn(r); err != nil {
			return fmt.Errorf("register trust/withdraw: %w", err)
		}
	}
	return nil
}

func init() { add(registerRoutes) }

// registerRoutes mounts the execution route through Registry.Routes, the only way an app
// mounts routes. Boot wiring supplies the Execution's dependencies; until it does, the route
// answers 503 and records nothing.
func registerRoutes(r registry.Registry) error {
	r.Routes((&Execution{}).mount)
	return nil
}

// mount is Routes with the route's write declaration.
func (x *Execution) mount(r chi.Router) {
	// rule: ARC-I-012
	// Execution is route-owned: it sets executed, never authorized (the trust.withdrawal effect does).
	r.Method(http.MethodPost, ExecutionPath, server.Declare(x.serve, "withdrawal.status=executed"))
}

// serve fails closed while any dependency is missing.
func (x *Execution) serve(w http.ResponseWriter, r *http.Request) {
	if x.DB == nil || x.Effect == nil || x.Bank == nil || x.Problems == nil || x.Principal == nil {
		pw := x.Problems
		if pw == nil {
			pw = &server.Problems{}
		}
		pw.WriteProblem(w, r, server.NewProblem(http.StatusServiceUnavailable, "unavailable", "Service not configured"))
		return
	}
	x.handle(w, r)
}

func init() { add(registerReadRoutes) }

// registerReadRoutes mounts GET /trust/withdrawals and GET /trust/withdrawals/{withdrawal_id}
// through Registry.Routes. Boot wiring supplies the dependencies; until it does, the routes
// answer 503.
func registerReadRoutes(r registry.Registry) error {
	r.Routes((&Reads{}).Routes)
	return nil
}

func init() { add(registerRequestRoutes) }

// registerRequestRoutes mounts POST /trust/withdrawals and POST
// /trust/withdrawals/{withdrawal_id}/cancel through Registry.Routes. Boot wiring supplies the
// dependencies; until it does, the routes answer 503.
func registerRequestRoutes(r registry.Registry) error {
	r.Routes((&RequestHTTP{}).Routes)
	return nil
}
