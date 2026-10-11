// Package flagsfake is an in-memory flags.Flags and flags.Validator for tests.
package flagsfake

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
)

var (
	_ flags.Flags     = (*Fake)(nil)
	_ flags.Validator = (*Fake)(nil)
)

// Fake keeps definitions and on/off state in maps. Every defined flag starts off.
type Fake struct {
	mu   sync.Mutex
	defs map[string]flags.Def
	on   map[string]bool
}

// New returns an empty Fake.
func New() *Fake { return &Fake{defs: map[string]flags.Def{}, on: map[string]bool{}} }

// Define implements flags.Flags.
func (f *Fake) Define(d flags.Def) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d.Name == "" {
		return fmt.Errorf("define: %w", flags.ErrUnknown)
	}
	if _, ok := f.defs[d.Name]; ok {
		return fmt.Errorf("define %q: %w", d.Name, flags.ErrDuplicate)
	}
	f.defs[d.Name] = d
	return nil
}

// Set implements flags.Flags.
func (f *Fake) Set(name string, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.defs[name]; !ok {
		return fmt.Errorf("set %q: %w", name, flags.ErrUnknown)
	}
	f.on[name] = on
	return nil
}

// Enabled implements flags.Flags.
func (f *Fake) Enabled(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.defs[name]
	return ok && f.on[name]
}

// Defs implements flags.Flags, sorted by name.
func (f *Fake) Defs() []flags.Def {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]flags.Def, 0, len(f.defs))
	for _, d := range f.defs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Validate implements flags.Validator.
func (f *Fake) Validate(_ context.Context, defs []flags.Def, today time.Time) error {
	var errs []error
	for _, d := range defs {
		// rule: ARC-U-019
		switch {
		case d.Owner == "":
			errs = append(errs, fmt.Errorf("flag %q: %w", d.Name, flags.ErrNoOwner))
		case d.Expiry.IsZero():
			errs = append(errs, fmt.Errorf("flag %q: %w", d.Name, flags.ErrNoExpiry))
		case d.Expiry.Before(today):
			errs = append(errs, fmt.Errorf("flag %q: %w", d.Name, flags.ErrExpired))
		}
	}
	return errors.Join(errs...)
}
