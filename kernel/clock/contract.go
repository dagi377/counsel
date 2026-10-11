// CONTRACT: kernel/clock types and interfaces. Frozen once on main; changes are additive only
// and need a docs/QUESTIONS.md entry. Source: spec/eng/k1-kernel.html (kernel/money, numseq, clock).

package clock

import (
	"context"
	"errors"
	"time"
)

// Date is a civil (calendar) date in the cell's business time zone, with no time of day.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// Clock is the source of the current instant and the business date. Production code reads
// time only through a Clock so tests can pin it.
type Clock interface {
	// Now returns the current instant.
	Now() time.Time
	// Today returns the business date of Now in Location.
	Today(ctx context.Context) Date
	// Location is the cell's business time zone (America/Toronto for every cell today).
	Location() *time.Location
}

// ErrNoClock is returned when a context carries no Clock and none can be built.
var ErrNoClock = errors.New("clock: no clock configured")

// ErrBadZone is returned when the configured business time zone cannot be loaded.
var ErrBadZone = errors.New("clock: unknown business time zone")
