package recon

import (
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

func day(y int, m time.Month, d int) clock.Date { return clock.Date{Year: y, Month: m, Day: d} }

// rule: BL9 s.18(8) (verify)
func TestDueDate(t *testing.T) {
	tests := []struct {
		name string
		p    Period
		want clock.Date
	}{
		{"31-day month", Period{2026, time.January}, day(2026, time.February, 25)},
		{"30-day month", Period{2026, time.April}, day(2026, time.May, 25)},
		{"february common year", Period{2026, time.February}, day(2026, time.March, 25)},
		{"february leap year", Period{2028, time.February}, day(2028, time.March, 25)},
		{"december crosses the year", Period{2026, time.December}, day(2027, time.January, 25)},
		{"due on a weekend does not roll", Period{2026, time.September}, day(2026, time.October, 25)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DueDate(tt.p); got != tt.want {
				t.Fatalf("DueDate(%v) = %s, want %s", tt.p, got, tt.want)
			}
		})
	}
}

// rule: BL9 s.18(8) (verify)
func TestOverdueAlerts(t *testing.T) {
	sep := Period{2026, time.September} // due 2026-10-25
	onTime, late := day(2026, time.October, 25), day(2026, time.October, 27)
	tests := []struct {
		name  string
		today clock.Date
		m     MonthStatus
		want  *DueAlert
	}{
		{"unsigned before due", day(2026, time.October, 12), MonthStatus{"a", sep, nil}, nil},
		{"unsigned on the due date", day(2026, time.October, 25), MonthStatus{"a", sep, nil}, nil},
		{"unsigned the day after", day(2026, time.October, 26), MonthStatus{"a", sep, nil},
			&DueAlert{"a", sep, day(2026, time.October, 25), 1, false}},
		{"unsigned a month later", day(2026, time.November, 25), MonthStatus{"a", sep, nil},
			&DueAlert{"a", sep, day(2026, time.October, 25), 31, false}},
		{"signed on the due date", day(2026, time.December, 1), MonthStatus{"a", sep, &onTime}, nil},
		{"signed late stays listed", day(2026, time.December, 1), MonthStatus{"a", sep, &late},
			&DueAlert{"a", sep, day(2026, time.October, 25), 2, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OverdueAlerts(tt.today, []MonthStatus{tt.m})
			switch {
			case tt.want == nil && len(got) != 0:
				t.Fatalf("alerts = %+v, want none", got)
			case tt.want != nil && (len(got) != 1 || got[0] != *tt.want):
				t.Fatalf("alerts = %+v, want %+v", got, *tt.want)
			}
		})
	}
}
