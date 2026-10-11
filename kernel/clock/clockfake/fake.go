// Package clockfake is an in-memory clock.Clock pinned to a settable instant.
package clockfake

import (
	"context"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

var _ clock.Clock = (*Fake)(nil)

// Fake is a clock.Clock whose instant only moves when the test sets or advances it.
type Fake struct {
	mu  sync.Mutex
	now time.Time
	loc *time.Location
}

// New returns a Fake at now in loc. A nil loc means UTC.
func New(now time.Time, loc *time.Location) *Fake {
	if loc == nil {
		loc = time.UTC
	}
	return &Fake{now: now, loc: loc}
}

// Now returns the pinned instant.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Today returns the date of the pinned instant in the fake's location.
func (f *Fake) Today(_ context.Context) clock.Date {
	t := f.Now().In(f.loc)
	return clock.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
}

// Location returns the fake's business time zone.
func (f *Fake) Location() *time.Location { return f.loc }

// Set pins the clock to t.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

// Advance moves the clock forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
