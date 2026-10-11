package credit

import (
	"fmt"

	"github.com/dagi377/counsel/kernel/registry"
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
			return fmt.Errorf("register billing/credit: %w", err)
		}
	}
	return nil
}

func init() { add(registerRoutes) }

// registerRoutes mounts GET /credit-notes, POST /credit-notes and
// GET /credit-notes/{credit_note_id} through Registry.Routes. Boot wiring supplies the
// dependencies; until it does, the routes answer 503.
func registerRoutes(r registry.Registry) error {
	r.Routes((&HTTP{}).Routes)
	return nil
}

func init() { add(registerWithdrawRoute) }

// registerWithdrawRoute mounts POST /credit-notes/{credit_note_id}/withdraw through
// Registry.Routes. Boot wiring supplies the dependencies; until it does, the route answers 503.
func registerWithdrawRoute(r registry.Registry) error {
	r.Routes((&WithdrawHTTP{}).Routes)
	return nil
}
