package docs

import (
	"fmt"
	"strings"
	"sync"
)

var _ Kinds = (*Registry)(nil)

// Registry holds the document kinds the app registers at boot. Which kinds exist is
// app data; the kernel only enforces their shape. It is safe for concurrent use.
type Registry struct {
	mu    sync.RWMutex
	kinds map[string]Kind
}

// NewRegistry returns an empty kind registry.
func NewRegistry() *Registry {
	return &Registry{kinds: map[string]Kind{}}
}

// Validate reports whether a kind is well formed: a module-prefixed id, a known
// classification and a retention class.
func (k Kind) Validate() error {
	mod, name, ok := strings.Cut(k.ID, ".")
	switch {
	case !ok || mod == "" || name == "":
		return fmt.Errorf("kind %q: id must be <module>.<kind>", k.ID)
	case !k.Classification.valid():
		return fmt.Errorf("kind %s: unknown classification %q", k.ID, k.Classification)
	case k.RetentionClass == "":
		return fmt.Errorf("kind %s: no retention class", k.ID)
	}
	return nil
}

func (c Classification) valid() bool {
	switch c {
	case Public, Internal, Confidential, Restricted:
		return true
	}
	return false
}

// RegisterKind implements Kinds. The stored kind owns a copy of Templates.
func (g *Registry) RegisterKind(k Kind) error {
	if err := k.Validate(); err != nil {
		return fmt.Errorf("register kind: %w", err)
	}
	k.Templates = append([]string(nil), k.Templates...)
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.kinds[k.ID]; ok {
		return fmt.Errorf("register kind %s: %w", k.ID, ErrDuplicateKind)
	}
	g.kinds[k.ID] = k
	return nil
}

// Kind implements Kinds. The returned Templates slice is a copy.
func (g *Registry) Kind(id string) (Kind, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	k, ok := g.kinds[id]
	if !ok {
		return Kind{}, fmt.Errorf("kind %s: %w", id, ErrUnknownKind)
	}
	k.Templates = append([]string(nil), k.Templates...)
	return k, nil
}
