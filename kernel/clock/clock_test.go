package clock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

func toronto(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(clock.DefaultZone)
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	return loc
}

func TestTodayInBusinessZone(t *testing.T) {
	loc := toronto(t)
	tests := []struct {
		name string
		now  time.Time
		want clock.Date
	}{
		{"after UTC midnight is previous business day", time.Date(2026, 11, 1, 3, 30, 0, 0, time.UTC), clock.Date{Year: 2026, Month: 10, Day: 31}},
		{"after business midnight", time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC), clock.Date{Year: 2026, Month: 11, Day: 1}},
		{"winter offset", time.Date(2027, 1, 1, 4, 59, 0, 0, time.UTC), clock.Date{Year: 2026, Month: 12, Day: 31}},
		{"winter midnight", time.Date(2027, 1, 1, 5, 0, 0, 0, time.UTC), clock.Date{Year: 2027, Month: 1, Day: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := clock.WithClock(context.Background(), clockfake.New(tt.now, loc))
			if got := clock.Today(ctx); got != tt.want {
				t.Fatalf("Today() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDayLength(t *testing.T) {
	loc := toronto(t)
	tests := []struct {
		name string
		day  clock.Date
		want time.Duration
	}{
		{"fall back", clock.Date{Year: 2026, Month: 11, Day: 1}, 25 * time.Hour},
		{"spring forward", clock.Date{Year: 2026, Month: 3, Day: 8}, 23 * time.Hour},
		{"ordinary", clock.Date{Year: 2026, Month: 10, Day: 31}, 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clock.DayLength(tt.day, loc); got != tt.want {
				t.Fatalf("DayLength(%v) = %v, want %v", tt.day, got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	toronto(t)
	tests := []struct {
		name    string
		zone    string
		want    string
		wantErr error
	}{
		{"empty is default", "", clock.DefaultZone, nil},
		{"explicit", "UTC", "UTC", nil},
		{"unknown", "Nowhere/Zone", "", clock.ErrBadZone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := clock.New(tt.zone)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("New(%q) err = %v, want %v", tt.zone, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got := c.Location().String(); got != tt.want {
				t.Fatalf("Location() = %q, want %q", got, tt.want)
			}
			if got, want := c.Today(context.Background()), clock.DateOf(time.Now(), c.Location()); got != want && got != want.AddDays(-1) {
				t.Fatalf("Today() = %v, want about %v", got, want)
			}
		})
	}
}

func TestFromFallsBackToDefault(t *testing.T) {
	toronto(t)
	c, err := clock.From(context.Background())
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if c.Location().String() != clock.DefaultZone {
		t.Fatalf("default zone = %v", c.Location())
	}
}

func TestDateHelpers(t *testing.T) {
	tests := []struct {
		name string
		d    clock.Date
		n    int
		want string
	}{
		{"month end", clock.Date{Year: 2026, Month: 10, Day: 31}, 1, "2026-11-01"},
		{"leap", clock.Date{Year: 2028, Month: 3, Day: 1}, -1, "2028-02-29"},
		{"zero", clock.Date{Year: 2026, Month: 1, Day: 5}, 0, "2026-01-05"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.AddDays(tt.n).String(); got != tt.want {
				t.Fatalf("AddDays = %s, want %s", got, tt.want)
			}
		})
	}
}
