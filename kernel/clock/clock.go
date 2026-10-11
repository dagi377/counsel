package clock

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DefaultZone is the business time zone used when a cell configures none.
const DefaultZone = "America/Toronto"

var _ Clock = (*System)(nil)

// System is the production Clock: wall-clock time, business dates in a configured zone.
type System struct {
	loc *time.Location
}

// New returns a System clock whose business dates are taken in zone (an IANA name).
// An empty zone means DefaultZone.
func New(zone string) (*System, error) {
	if zone == "" {
		zone = DefaultZone
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("load zone %q: %w: %v", zone, ErrBadZone, err)
	}
	return &System{loc: loc}, nil
}

// Now returns the current instant.
func (s *System) Now() time.Time { return time.Now() }

// Today returns the business date of Now in the configured zone.
func (s *System) Today(_ context.Context) Date { return DateOf(s.Now(), s.loc) }

// Location returns the configured business time zone.
func (s *System) Location() *time.Location { return s.loc }

// DateOf returns the civil date of t in loc.
func DateOf(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()
	return Date{Year: y, Month: m, Day: d}
}

// Start returns the first instant of d in loc.
func (d Date) Start(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

// AddDays returns the date n calendar days after d (n may be negative).
func (d Date) AddDays(n int) Date {
	return DateOf(time.Date(d.Year, d.Month, d.Day+n, 12, 0, 0, 0, time.UTC), time.UTC)
}

// String formats d as YYYY-MM-DD.
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// DayLength returns the elapsed time of d in loc: 24 h except on daylight-saving change days.
func DayLength(d Date, loc *time.Location) time.Duration {
	return d.AddDays(1).Start(loc).Sub(d.Start(loc))
}

type ctxKey struct{}

// WithClock returns a context that carries c.
func WithClock(ctx context.Context, c Clock) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

var (
	defaultMu  sync.Mutex
	defaultClk Clock
)

// SetDefault sets the Clock used when a context carries none. Boot calls it once.
func SetDefault(c Clock) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultClk = c
}

// From returns the Clock carried by ctx, else the default Clock, else a System clock in
// DefaultZone. It returns ErrNoClock only if that zone cannot be loaded.
func From(ctx context.Context) (Clock, error) {
	if c, ok := ctx.Value(ctxKey{}).(Clock); ok && c != nil {
		return c, nil
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultClk != nil {
		return defaultClk, nil
	}
	s, err := New(DefaultZone)
	if err != nil {
		return nil, fmt.Errorf("default clock: %w: %w", ErrNoClock, err)
	}
	defaultClk = s
	return s, nil
}

// Today returns the business date for ctx: the cell's date in its configured zone.
// It panics only if no Clock exists and the default zone's tzdata is missing, which boot
// rules out by building the Clock with New first.
func Today(ctx context.Context) Date {
	c, err := From(ctx)
	if err != nil {
		panic(err)
	}
	return c.Today(ctx)
}
