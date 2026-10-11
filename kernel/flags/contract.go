// CONTRACT: kernel/flags (T-044). Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Source: spec/eng/a1-uptime.json ARC-U-019. Def, Set, ErrUnknown
// and ErrDuplicate live in flags.go; config validation bodies live in T-044.3 (config.go).

package flags

import (
	"context"
	"errors"
	"time"
)

// Config load errors (ARC-U-019). Each wraps the flag name.
var (
	// ErrNoOwner means a flag definition has no owner.
	ErrNoOwner = errors.New("flags: flag has no owner")
	// ErrNoExpiry means a flag definition has no expiry date.
	ErrNoExpiry = errors.New("flags: flag has no expiry")
	// ErrExpired means a flag is past its expiry date; CI fails on it.
	ErrExpired = errors.New("flags: flag past expiry")
)

// Flags is the read and write surface of a flag set. *Set implements it.
type Flags interface {
	Define(d Def) error
	Set(name string, on bool) error
	Enabled(name string) bool
	Defs() []Def
}

// Validator checks flag definitions at config load. today is the business date
// (clock.Today); a flag whose Expiry is before today returns ErrExpired.
type Validator interface {
	Validate(ctx context.Context, defs []Def, today time.Time) error
}

var _ Flags = (*Set)(nil)
