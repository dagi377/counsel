package recon

import (
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

// DueDays is how many days after month end the monthly trust comparison must be done.
// (verify) By-law 9 s.18(8): within 25 days of the end of the month.
const DueDays = 25

// DueDate is the last day to complete and sign the reconciliation for month p: the last
// day of the month plus DueDays, as an America/Toronto calendar date. It does not roll off
// weekends or holidays (the earlier date, fail closed).
func DueDate(p Period) clock.Date {
	// rule: BL9 s.18(8) (verify)
	last := time.Date(p.Year, p.Month+1, 0, 0, 0, 0, 0, time.UTC)
	return clock.Date{Year: last.Year(), Month: last.Month(), Day: last.Day()}.AddDays(DueDays)
}

// MonthStatus is one trust account's reconciliation state for one month.
type MonthStatus struct {
	AccountID string
	Period    Period
	// ApprovedOn is the America/Toronto date the reconciliation was signed off; nil while it
	// is not approved.
	ApprovedOn *clock.Date
}

// DueAlert is a month whose reconciliation is overdue (not signed by its due date) or was
// signed late. Late sign-offs stay listed so the record shows the miss.
type DueAlert struct {
	AccountID string
	Period    Period
	Due       clock.Date
	// DaysLate counts days after Due: to today while unsigned, to the sign-off when signed.
	DaysLate int
	Signed   bool
}

// OverdueAlerts returns an alert for every month in months that was not signed off by its
// DueDate as of today, in input order. A month still within its 25 days gives no alert.
func OverdueAlerts(today clock.Date, months []MonthStatus) []DueAlert {
	var out []DueAlert
	for _, m := range months {
		due := DueDate(m.Period)
		// rule: BL9 s.18(8) (verify)
		end, signed := today, m.ApprovedOn != nil
		if signed {
			end = *m.ApprovedOn
		}
		if !due.Before(end) {
			continue
		}
		out = append(out, DueAlert{AccountID: m.AccountID, Period: m.Period, Due: due, DaysLate: daysBetween(due, end), Signed: signed})
	}
	return out
}

func daysBetween(a, b clock.Date) int {
	return int(b.Start(time.UTC).Sub(a.Start(time.UTC)).Hours() / 24)
}
