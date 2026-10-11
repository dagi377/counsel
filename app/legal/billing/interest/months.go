package interest

import "time"

var _ Calendar = Months{}

// Months is the calendar-month arithmetic behind interest accrual. Loc is the business
// time zone (America/Toronto); a delivery instant is turned into a Toronto date, never a
// UTC one. Accrual that uses it ships behind billing.solicitors_act_verified (off by default).
type Months struct {
	Loc *time.Location
}

// MonthsAfter adds n calendar months to the business date d, clamping to the last day of
// the target month (2026-01-31 + 1 = 2026-02-28). The result is midnight in d's zone.
func (m Months) MonthsAfter(d time.Time, n int) time.Time {
	y, mo, day := d.Date()
	first := time.Date(y, mo+time.Month(n), 1, 0, 0, 0, 0, d.Location())
	if last := first.AddDate(0, 1, -1).Day(); day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, d.Location())
}

// AccrualStart is MonthsAfter(delivery date in Loc, 1): Solicitors Act s.33 runs interest from
// one month after delivery (delivered 01-15, interest from 02-15; BIL-C-073, BIL-L-008).
// rule: BIL-L-008 (verify)
func (m Months) AccrualStart(deliveredAt time.Time) time.Time {
	y, mo, d := deliveredAt.In(m.Loc).Date()
	local := time.Date(y, mo, d, 0, 0, 0, 0, m.Loc)
	return m.MonthsAfter(local, 1)
}
