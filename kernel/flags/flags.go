package flags

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrUnknown is returned when a flag is turned on or off before it was defined.
var ErrUnknown = errors.New("flags: unknown flag")

// ErrDuplicate is returned when a flag name is defined twice.
var ErrDuplicate = errors.New("flags: duplicate flag")

// Def describes one feature flag. The app defines its flags at boot; the kernel defines none.
type Def struct {
	Name   string    // dotted name, e.g. "billing.example"
	Owner  string    // who decides when the flag is turned on
	Expiry time.Time // when the flag must be removed or made permanent
	Doc    string    // one line: what the flag gates
	On     bool      // initial state; false (off) unless a recorded decision says otherwise
}

// Set holds flag definitions and which of them are on. A defined flag starts in its Def.On
// state, which is off unless the definition says otherwise.
// A Set is safe for concurrent use.
type Set struct {
	mu   sync.RWMutex
	defs map[string]Def
	on   map[string]bool
}

// New returns an empty Set.
func New() *Set {
	return &Set{defs: map[string]Def{}, on: map[string]bool{}}
}

// Define adds a flag, off. Defining the same name twice is an error.
func (s *Set) Define(d Def) error {
	if d.Name == "" {
		return fmt.Errorf("define flag: empty name: %w", ErrUnknown)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.defs[d.Name]; ok {
		return fmt.Errorf("define flag %q: %w", d.Name, ErrDuplicate)
	}
	s.defs[d.Name] = d
	if d.On {
		s.on[d.Name] = true
	}
	return nil
}

// Set turns a defined flag on or off.
func (s *Set) Set(name string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.defs[name]; !ok {
		return fmt.Errorf("set flag %q: %w", name, ErrUnknown)
	}
	s.on[name] = on
	return nil
}

// Enabled reports whether name is defined and on.
func (s *Set) Enabled(name string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, defined := s.defs[name]
	return defined && s.on[name]
}

// Defs returns every definition, sorted by name.
func (s *Set) Defs() []Def {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Def, 0, len(s.defs))
	for _, d := range s.defs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type ctxKey struct{}

// WithSet returns a context that carries s; Enabled reads it.
func WithSet(ctx context.Context, s *Set) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// FromContext returns the Set carried by ctx, or nil.
func FromContext(ctx context.Context) *Set {
	s, _ := ctx.Value(ctxKey{}).(*Set)
	return s
}

// Enabled reports whether the flag name is on in the Set carried by ctx. It is false for any
// flag that is undefined or not explicitly turned on, and false when ctx carries no Set.
func Enabled(ctx context.Context, name string) bool {
	return FromContext(ctx).Enabled(name)
}
