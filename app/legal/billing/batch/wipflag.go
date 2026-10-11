package batch

import (
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

// FlagUndueDelay is the WIP report flag for work held unbilled past its billing cycle.
const FlagUndueDelay = "undue_delay"

// undueDelayGraceDays is added to the matter's billing cycle before WIP is flagged.
const undueDelayGraceDays = 30

// WIPEntry is one approved, unbilled time entry on a WIP report.
type WIPEntry struct {
	EntryID          string
	MatterID         string
	Dated            clock.Date
	BillingCycleDays int // the matter's billing cycle, e.g. 30 for monthly
}

// UndueDelay reports whether e has been held unbilled past the matter's billing cycle plus
// 30 days as of today, so tax is not deferred by holding bills. It is a report flag only;
// nothing is billed automatically.
//
// rule: BIL-T-052
func UndueDelay(e WIPEntry, today clock.Date) bool {
	limit := civil(e.Dated).AddDate(0, 0, e.BillingCycleDays+undueDelayGraceDays)
	return civil(today).After(limit)
}

// FlagWIP returns the ids of the entries UndueDelay flags, in input order.
func FlagWIP(entries []WIPEntry, today clock.Date) []string {
	var out []string
	for _, e := range entries {
		// rule: BIL-T-052
		if UndueDelay(e, today) {
			out = append(out, e.EntryID)
		}
	}
	return out
}

// civil turns a business date into a UTC midnight for calendar arithmetic only.
func civil(d clock.Date) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}
