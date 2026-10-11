// Package legal is the Counsel app. It registers every legal module with the kernel at boot.
package legal

import (
	"fmt"

	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/webhook"
)

// Register registers every app/legal module with the kernel registry, in the order of modules.
// It stops at the first module that fails.
//
// Registrations come in two sets (Q-005). The "always" set (chart of accounts, core gate
// kinds and capabilities) registers on every boot. Everything else is feature-gated: a
// feature registers only when its flag is on, so with no features on only the always set
// is recorded.
func Register(r registry.Registry) error {
	for _, m := range modules {
		if err := m.register(r); err != nil {
			return fmt.Errorf("legal: register %s: %w", m.name, err)
		}
	}
	registerKernelRoutes(r)
	return nil
}

// registerKernelRoutes mounts the kernel-owned routes the app serves: GET /webhook-endpoints,
// POST /webhook-endpoints and PATCH /webhook-endpoints/{webhook_id}. Boot wiring supplies their
// dependencies; until it does, the routes answer 503.
func registerKernelRoutes(r registry.Registry) {
	r.Routes((&webhook.HTTP{}).Routes)
}
