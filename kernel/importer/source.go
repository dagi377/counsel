package importer

import (
	"fmt"
	"sort"
	"sync"
)

// Sources holds the sources the app registered at boot, keyed by source id.
// It is safe for concurrent use.
type Sources struct {
	mu   sync.RWMutex
	byID map[string]Source
}

// NewSources returns an empty source set.
func NewSources() *Sources { return &Sources{byID: map[string]Source{}} }

// Register adds s under id. An id can be registered once.
func (r *Sources) Register(id string, s Source) error {
	if id == "" || s == nil {
		return fmt.Errorf("register source %q: id and source are required", id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[id]; ok {
		return fmt.Errorf("register source %q: %w", id, ErrDuplicateSource)
	}
	r.byID[id] = s
	return nil
}

// Get returns the source registered under id.
func (r *Sources) Get(id string) (Source, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.byID[id]
	if !ok {
		return nil, fmt.Errorf("source %q: %w", id, ErrUnknownSource)
	}
	return s, nil
}

// IDs returns the registered source ids in ascending order.
func (r *Sources) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
