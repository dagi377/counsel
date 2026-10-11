package flags

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ConfigValidator is the Validator used at config load and in CI.
type ConfigValidator struct{}

var _ Validator = ConfigValidator{}

// Validate checks every definition and joins all failures, so one run lists every bad flag.
// A flag expiring on today is still valid; a flag whose expiry is before today is not.
func (ConfigValidator) Validate(ctx context.Context, defs []Def, today time.Time) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("validate flags: %w", err)
	}
	day := dateOf(today)
	var errs []error
	for _, d := range defs {
		// rule: ARC-U-019
		switch {
		case d.Owner == "":
			errs = append(errs, fmt.Errorf("flag %q: %w", d.Name, ErrNoOwner))
		case d.Expiry.IsZero():
			errs = append(errs, fmt.Errorf("flag %q: %w", d.Name, ErrNoExpiry))
		case dateOf(d.Expiry).Before(day):
			errs = append(errs, fmt.Errorf("flag %q expired %s: %w", d.Name, d.Expiry.Format(time.DateOnly), ErrExpired))
		}
	}
	return errors.Join(errs...)
}

// Load validates defs and defines each on a new Set, all off. Any invalid flag fails the load.
func Load(ctx context.Context, defs []Def, today time.Time) (*Set, error) {
	// rule: ARC-U-019
	if err := (ConfigValidator{}).Validate(ctx, defs, today); err != nil {
		return nil, fmt.Errorf("load flags: %w", err)
	}
	s := New()
	for _, d := range defs {
		if err := s.Define(d); err != nil {
			return nil, fmt.Errorf("load flags: %w", err)
		}
	}
	return s, nil
}

// dateOf drops the clock time, keeping the calendar date in t's own location.
func dateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
