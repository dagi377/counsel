package access

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
			return fmt.Errorf("register access: %w", err)
		}
	}
	return nil
}

func init() { add(registerAdminRoutes) }

// registerAdminRoutes mounts GET /users, PUT /users/{user_id}/roles, GET /roles and
// GET /audit-events through Registry.Routes. Boot wiring supplies the read ports, the masker
// and the gate engine; until it does, the routes answer 503.
func registerAdminRoutes(r registry.Registry) error {
	r.Routes((&HTTP{}).Routes)
	return nil
}
