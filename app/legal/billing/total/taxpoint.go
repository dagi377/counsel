package total

import (
	"time"
	_ "time/tzdata" // the business zone must resolve on hosts without a zoneinfo database
)

// businessZone is America/Toronto, the zone every tax point and filing period is read in.
var businessZone = mustZone("America/Toronto")

func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// businessDay returns midnight, America/Toronto, of the calendar day t falls on there.
// A zero t stays zero.
func businessDay(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	y, m, d := t.In(businessZone).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, businessZone)
}

// TaxPoint returns the day tax on a bill becomes payable, as midnight America/Toronto.
// Every input is first read as a calendar day in America/Toronto, so an instant stored in
// UTC lands on its local day.
//
// rule: BIL-T-050
// The tax point is the earlier of the day the consideration is paid and the day it becomes
// due, and it becomes due no later than the bill date (the approval date, BIL-C-121).
func TaxPoint(in TaxPointInput) (time.Time, error) {
	due := businessDay(in.ApprovedOn)
	// rule: BIL-T-050
	if dueOn := businessDay(in.DueOn); !dueOn.IsZero() && (due.IsZero() || dueOn.Before(due)) {
		due = dueOn
	}
	paid := businessDay(in.PaidOn)
	switch {
	case due.IsZero() && paid.IsZero():
		return time.Time{}, ErrNoTaxPoint
	case due.IsZero():
		return paid, nil
	case paid.IsZero() || !paid.Before(due):
		return due, nil
	default:
		return paid, nil
	}
}
