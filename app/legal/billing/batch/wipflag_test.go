package batch

import (
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

func date(y int, m time.Month, d int) clock.Date { return clock.Date{Year: y, Month: m, Day: d} }

// rule: BIL-T-052
func TestUndueDelay(t *testing.T) {
	tests := []struct {
		name  string
		e     WIPEntry
		today clock.Date
		want  bool
	}{
		{"spec: monthly, dated 07-10, unbilled 09-15", WIPEntry{Dated: date(2026, time.July, 10), BillingCycleDays: 30}, date(2026, time.September, 15), true},
		{"on the last allowed day", WIPEntry{Dated: date(2026, time.July, 10), BillingCycleDays: 30}, date(2026, time.September, 8), false},
		{"day after the limit", WIPEntry{Dated: date(2026, time.July, 10), BillingCycleDays: 30}, date(2026, time.September, 9), true},
		{"quarterly cycle not yet late", WIPEntry{Dated: date(2026, time.July, 10), BillingCycleDays: 90}, date(2026, time.September, 15), false},
		{"recent work", WIPEntry{Dated: date(2026, time.September, 1), BillingCycleDays: 30}, date(2026, time.September, 15), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := UndueDelay(tc.e, tc.today); got != tc.want {
				t.Fatalf("UndueDelay = %v, want %v", got, tc.want)
			}
		})
	}
}

// rule: BIL-T-052
func TestFlagWIP(t *testing.T) {
	entries := []WIPEntry{
		{EntryID: "old", Dated: date(2026, time.July, 10), BillingCycleDays: 30},
		{EntryID: "new", Dated: date(2026, time.September, 1), BillingCycleDays: 30},
	}
	if got := FlagWIP(entries, date(2026, time.September, 15)); !reflect.DeepEqual(got, []string{"old"}) {
		t.Fatalf("FlagWIP = %v", got)
	}
}
