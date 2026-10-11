package deadline

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidRule means a rule cannot be computed, for example a negative business-day offset.
var ErrInvalidRule = errors.New("deadline: invalid rule")

// Due returns the due date of rule ruleID of p for an event on trigger. It applies the
// months first (clamped to the target month's last day), then the days by day-count mode,
// then the roll rule. Weekends and the pack's holidays are not business days.
func Due(p Pack, ruleID string, trigger Date) (Date, error) {
	for _, r := range p.Rules {
		if r.ID == ruleID {
			return dueFor(p, r, trigger)
		}
	}
	return Date{}, fmt.Errorf("due %s/%s: %w", p.ID, ruleID, ErrUnknownRule)
}

// IsBusinessDay reports whether d is a weekday that is not one of holidays.
func IsBusinessDay(d Date, holidays []Date) bool {
	switch d.Start(time.UTC).Weekday() {
	case time.Saturday, time.Sunday:
		return false
	}
	for _, h := range holidays {
		if h == d {
			return false
		}
	}
	return true
}

func dueFor(p Pack, r Rule, trigger Date) (Date, error) {
	if r.MonthsAfter < 0 {
		return Date{}, fmt.Errorf("due %s/%s: negative months: %w", p.ID, r.ID, ErrInvalidRule)
	}
	d := addMonths(trigger, r.MonthsAfter)
	switch r.Mode {
	case Calendar:
		d = d.AddDays(r.Days)
	case Clear:
		// Neither the trigger day nor the due day counts: N clear days end on trigger + N + 1.
		if r.Days < 0 {
			return Date{}, fmt.Errorf("due %s/%s: negative clear days: %w", p.ID, r.ID, ErrInvalidRule)
		}
		d = d.AddDays(r.Days + 1)
	case Business:
		if r.Days < 0 {
			return Date{}, fmt.Errorf("due %s/%s: negative business days: %w", p.ID, r.ID, ErrInvalidRule)
		}
		for n := 0; n < r.Days; {
			d = d.AddDays(1)
			if IsBusinessDay(d, p.Holidays) {
				n++
			}
		}
	default:
		return Date{}, fmt.Errorf("due %s/%s: day count %q: %w", p.ID, r.ID, r.Mode, ErrInvalidRule)
	}
	switch r.Roll {
	case RollNone, "":
	case RollForward:
		for !IsBusinessDay(d, p.Holidays) {
			d = d.AddDays(1)
		}
	default:
		return Date{}, fmt.Errorf("due %s/%s: roll %q: %w", p.ID, r.ID, r.Roll, ErrInvalidRule)
	}
	return d, nil
}

// addMonths adds n calendar months to d, clamping to the last day of the target month.
func addMonths(d Date, n int) Date {
	if n == 0 {
		return d
	}
	first := time.Date(d.Year, d.Month+time.Month(n), 1, 12, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	day := d.Day
	if day > last {
		day = last
	}
	return Date{Year: first.Year(), Month: first.Month(), Day: day}
}
