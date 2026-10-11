package deadline_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/deadline"
)

// testPack is a non-legal pack (helpdesk holidays) with one rule per mode and roll.
func testPack(t *testing.T) deadline.Pack {
	t.Helper()
	p := helpdesk(t)
	p.Rules = append(p.Rules,
		deadline.Rule{ID: "calendar_7", Mode: deadline.Calendar, Days: 7, Roll: deadline.RollNone},
		deadline.Rule{ID: "calendar_7_fwd", Mode: deadline.Calendar, Days: 7, Roll: deadline.RollForward},
		deadline.Rule{ID: "clear_2", Mode: deadline.Clear, Days: 2, Roll: deadline.RollNone},
		deadline.Rule{ID: "clear_2_fwd", Mode: deadline.Clear, Days: 2, Roll: deadline.RollForward},
		deadline.Rule{ID: "business_5", Mode: deadline.Business, Days: 5, Roll: deadline.RollNone},
		deadline.Rule{ID: "business_0_fwd", Mode: deadline.Business, Days: 0, Roll: deadline.RollForward},
		deadline.Rule{ID: "month_1", Mode: deadline.Calendar, MonthsAfter: 1, Roll: deadline.RollNone},
		deadline.Rule{ID: "month_1_plus_2", Mode: deadline.Calendar, MonthsAfter: 1, Days: 2, Roll: deadline.RollNone},
		deadline.Rule{ID: "neg_business", Mode: deadline.Business, Days: -1},
		deadline.Rule{ID: "bad_mode", Mode: "lunar", Days: 1},
	)
	return p
}

func TestDueGoldenDates(t *testing.T) {
	p := testPack(t)
	tests := []struct {
		name    string
		rule    string
		trigger deadline.Date
		want    deadline.Date
	}{
		// calendar: trigger + N, no roll even onto a holiday.
		{"calendar plain", "calendar_7", date(2026, time.March, 2), date(2026, time.March, 9)},
		{"calendar onto holiday no roll", "calendar_7", date(2026, time.December, 18), date(2026, time.December, 25)},
		{"calendar onto holiday rolls past weekend", "calendar_7_fwd", date(2026, time.December, 18), date(2026, time.December, 28)},
		{"calendar across year end", "calendar_7", date(2026, time.December, 28), date(2027, time.January, 4)},
		{"helpdesk auto close", "auto_close", date(2026, time.December, 11), date(2026, time.December, 25)},
		// clear: trigger + N + 1.
		{"clear plain", "clear_2", date(2026, time.March, 2), date(2026, time.March, 5)},
		{"clear lands saturday no roll", "clear_2", date(2026, time.March, 4), date(2026, time.March, 7)},
		{"clear lands saturday rolls monday", "clear_2_fwd", date(2026, time.March, 4), date(2026, time.March, 9)},
		{"clear onto new year rolls", "clear_2_fwd", date(2026, time.December, 29), date(2027, time.January, 4)},
		// business: weekdays not holidays.
		{"business within week", "business_5", date(2026, time.March, 2), date(2026, time.March, 9)},
		{"business over weekend", "business_5", date(2026, time.March, 5), date(2026, time.March, 12)},
		{"business over holiday and weekend", "business_5", date(2026, time.December, 23), date(2026, time.December, 31)},
		{"business over new year", "business_5", date(2026, time.December, 30), date(2027, time.January, 7)},
		{"helpdesk first response friday", "first_response", date(2026, time.March, 6), date(2026, time.March, 9)},
		{"helpdesk first response before holiday", "first_response", date(2026, time.December, 24), date(2026, time.December, 28)},
		{"business zero on holiday rolls", "business_0_fwd", date(2026, time.December, 25), date(2026, time.December, 28)},
		{"business zero on business day stays", "business_0_fwd", date(2026, time.March, 2), date(2026, time.March, 2)},
		// months: clamp to month end, then days.
		{"month plain", "month_1", date(2026, time.March, 15), date(2026, time.April, 15)},
		{"month clamps jan 31", "month_1", date(2026, time.January, 31), date(2026, time.February, 28)},
		{"month clamps leap year", "month_1", date(2028, time.January, 31), date(2028, time.February, 29)},
		{"month across year", "month_1", date(2026, time.December, 31), date(2027, time.January, 31)},
		{"month then days", "month_1_plus_2", date(2026, time.January, 31), date(2026, time.March, 2)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := deadline.Due(p, tc.rule, tc.trigger)
			if err != nil {
				t.Fatalf("Due: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Due(%s, %s) = %s, want %s", tc.rule, tc.trigger, got, tc.want)
			}
		})
	}
}

func TestDueErrors(t *testing.T) {
	p := testPack(t)
	tests := []struct {
		name string
		rule string
		want error
	}{
		{"unknown rule", "nope", deadline.ErrUnknownRule},
		{"negative business days", "neg_business", deadline.ErrInvalidRule},
		{"unknown mode", "bad_mode", deadline.ErrInvalidRule},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := deadline.Due(p, tc.rule, date(2026, time.March, 2)); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestIsBusinessDay(t *testing.T) {
	hol := []deadline.Date{date(2026, time.December, 25)}
	tests := []struct {
		d    deadline.Date
		want bool
	}{
		{date(2026, time.December, 24), true},
		{date(2026, time.December, 25), false},
		{date(2026, time.December, 26), false},
		{date(2026, time.December, 27), false},
		{date(2026, time.December, 28), true},
	}
	for _, tc := range tests {
		if got := deadline.IsBusinessDay(tc.d, hol); got != tc.want {
			t.Errorf("IsBusinessDay(%s) = %t, want %t", tc.d, got, tc.want)
		}
	}
}
