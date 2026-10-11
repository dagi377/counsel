package recon

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
			return fmt.Errorf("register trust/recon: %w", err)
		}
	}
	return nil
}

func init() { add(registerReadRoutes) }

// registerReadRoutes mounts GET /trust/reconciliations through Registry.Routes. Boot wiring
// supplies the dependencies; until it does, the route answers 503.
func registerReadRoutes(r registry.Registry) error {
	r.Routes((&Reads{}).Routes)
	return nil
}

func init() { add(registerWriteRoutes) }

// registerWriteRoutes mounts POST /trust/bank-lines/imports, POST /trust/reconciliations and
// POST /trust/reconciliations/{reconciliation_id}/submit through Registry.Routes. Boot wiring
// supplies the dependencies; until it does, the routes answer 503.
func registerWriteRoutes(r registry.Registry) error {
	r.Routes((&WriteHTTP{}).Routes)
	return nil
}
