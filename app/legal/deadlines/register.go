package deadlines

import "github.com/dagi377/counsel/kernel/registry"

// Register mounts the trigger route (trigger_http.go); boot wiring supplies the dependencies
// and until it does the route answers 503. The AI features do not register here: boot adds
// them to the feature.Set explicitly.
func Register(r registry.Registry) error {
	r.Routes((&TriggerHTTP{}).Routes)
	return nil
}
