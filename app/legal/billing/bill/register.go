package bill

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
			return fmt.Errorf("register billing/bill: %w", err)
		}
	}
	return nil
}

func init() { add(registerReadRoutes) }

// registerReadRoutes mounts GET /bills, GET /bills/{bill_id} and GET /bills/{bill_id}/lines
// through Registry.Routes. Boot wiring supplies the dependencies; until it does, the routes
// answer 503.
func registerReadRoutes(r registry.Registry) error {
	r.Routes((&HTTP{}).Routes)
	return nil
}

func init() { add(registerDraftRoutes) }

// registerDraftRoutes mounts POST /bills and PATCH /bills/{bill_id} through Registry.Routes.
// Boot wiring supplies the dependencies; until it does, the routes answer 503.
func registerDraftRoutes(r registry.Registry) error {
	r.Routes((&DraftHTTP{}).Routes)
	return nil
}

func init() { add(registerActionRoutes) }

// registerActionRoutes mounts POST /bills/{bill_id}/submit, /void and /deliver through
// Registry.Routes. Boot wiring supplies the dependencies; until it does, the routes answer
// 503.
func registerActionRoutes(r registry.Registry) error {
	r.Routes((&ActionHTTP{}).Routes)
	return nil
}
