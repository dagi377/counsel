package interest

import (
	"testing"
	"time"
)

func toronto(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	return loc
}

func TestMonthsAfter(t *testing.T) {
	loc := toronto(t)
	m := Months{Loc: loc}
	day := func(y int, mo time.Month, d int) time.Time { return time.Date(y, mo, d, 0, 0, 0, 0, loc) }
	tests := []struct {
		name string
		in   time.Time
		n    int
		want time.Time
	}{
		{"jan 31 clamps to feb 28", day(2026, 1, 31), 1, day(2026, 2, 28)},
		{"mar 31 clamps to apr 30", day(2026, 3, 31), 1, day(2026, 4, 30)},
		{"leap year jan 30 to feb 29", day(2028, 1, 30), 1, day(2028, 2, 29)},
		{"mid month", day(2026, 5, 15), 1, day(2026, 6, 15)},
		{"crosses year", day(2026, 12, 31), 2, day(2027, 2, 28)},
		{"zero months", day(2026, 7, 4), 0, day(2026, 7, 4)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.MonthsAfter(tt.in, tt.n); !got.Equal(tt.want) {
				t.Fatalf("MonthsAfter = %s, want %s", got.Format(time.DateOnly), tt.want.Format(time.DateOnly))
			}
		})
	}
}

func TestAccrualStartUsesTorontoDate(t *testing.T) {
	loc := toronto(t)
	m := Months{Loc: loc}
	tests := []struct {
		name      string
		delivered time.Time
		wantMonth string // MonthsAfter(local delivery date, 1)
	}{
		{"utc next day is toronto oct 31", time.Date(2026, 11, 1, 3, 30, 0, 0, time.UTC), "2026-11-30"},
		{"jan 31 local", time.Date(2026, 1, 31, 12, 0, 0, 0, loc), "2026-02-28"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.AccrualStart(tt.delivered)
			if got.Location() != loc {
				t.Fatalf("zone = %s, want America/Toronto", got.Location())
			}
			wantStart := m.MonthsAfter(mustDate(t, tt.wantMonth, loc), 0).AddDate(0, 0, 1)
			if !got.Equal(wantStart) {
				t.Fatalf("AccrualStart = %s, want day after %s", got.Format(time.DateOnly), tt.wantMonth)
			}
		})
	}
}

func mustDate(t *testing.T, s string, loc *time.Location) time.Time {
	t.Helper()
	d, err := time.ParseInLocation(time.DateOnly, s, loc)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return d
}
