package retention

import (
	"fmt"
	"sync"
	"time"
)

var _ Classes = (*Registry)(nil)

// Registry holds the retention classes the app registers at boot. Classes are app
// data; the kernel only enforces their shape. It is safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	classes map[string]Class
}

// NewRegistry returns an empty class registry.
func NewRegistry() *Registry {
	return &Registry{classes: map[string]Class{}}
}

// Validate reports whether a class is well formed: it has an id, a non-negative
// minimum, and a destruction gate unless it is never purged.
func (c Class) Validate() error {
	switch {
	case c.ID == "":
		return fmt.Errorf("class: empty id: %w", ErrInvalidClass)
	case c.MinYears < 0:
		return fmt.Errorf("class %s: negative min_years: %w", c.ID, ErrInvalidClass)
	case !c.NeverPurge && c.DestructionGate == "":
		return fmt.Errorf("class %s: no destruction gate: %w", c.ID, ErrInvalidClass)
	}
	return nil
}

// Due reports whether a record of this class is past its minimum retention on asOf
// and may be proposed for purge. A hold, a never-purge class, an unset ClosedOn or a
// record of another class is never due. Due proposes nothing and destroys nothing.
func (c Class) Due(r Record, asOf time.Time) bool {
	if c.NeverPurge || r.Class != c.ID || r.Hold || r.ClosedOn.IsZero() {
		return false
	}
	return !r.ClosedOn.AddDate(c.MinYears, 0, 0).After(asOf)
}

// RegisterClass implements Classes.
func (g *Registry) RegisterClass(c Class) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("register class: %w", err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.classes[c.ID]; ok {
		return fmt.Errorf("register class %s: %w", c.ID, ErrDuplicateClass)
	}
	g.classes[c.ID] = c
	return nil
}

// Class implements Classes.
func (g *Registry) Class(id string) (Class, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c, ok := g.classes[id]
	if !ok {
		return Class{}, fmt.Errorf("class %s: %w", id, ErrUnknownClass)
	}
	return c, nil
}
