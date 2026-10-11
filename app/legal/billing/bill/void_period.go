package bill

import (
	"fmt"
	"time"
)

// VoidTaxKind is how the tax on a voided bill reaches the sales tax report.
type VoidTaxKind string

// Void tax kinds.
const (
	// VoidTaxNets is a reversal while the bill-date period is still unfiled: the reversal,
	// dated the void date, offsets the bill's tax, and inside that same period the two net
	// to zero.
	VoidTaxNets VoidTaxKind = "nets_in_period"
	// VoidTaxAdjustment is a negative tax adjustment reported in the void period because the
	// bill-date period was already filed. The filed period is not reopened.
	VoidTaxAdjustment VoidTaxKind = "negative_adjustment"
)

// WarnPeriodCross is the preflight warning code for an approved bill still undelivered at
// the end of a tax period.
const WarnPeriodCross = "W-PERIOD-CROSS"

// VoidTax is the tax effect of a void.
type VoidTax struct {
	Kind VoidTaxKind
	// On is the business date the reversal is dated and reported on: always the void date.
	On time.Time
}

// PeriodWarning is one preflight warning for a bill at a period end.
type PeriodWarning struct {
	Code   string
	Bill   ID
	Number string
}

// VoidTaxEffect returns the tax effect of voiding a bill dated billDate on voidDate.
// billPeriodFiled reports whether the tax period holding billDate is already filed.
// It returns ErrInvalid for a zero date or a void before the bill date.
func VoidTaxEffect(billDate, voidDate time.Time, billPeriodFiled bool) (VoidTax, error) {
	if billDate.IsZero() || voidDate.IsZero() || periodDate(voidDate).Before(periodDate(billDate)) {
		return VoidTax{}, fmt.Errorf("void tax effect: %w", ErrInvalid)
	}
	// rule: BIL-C-123
	// The reversal is dated the void date. If the bill-date period is filed, it is a
	// negative adjustment in the void period and the filed period is not reopened; inside
	// the same unfiled period the bill and its reversal net to zero.
	if billPeriodFiled {
		return VoidTax{Kind: VoidTaxAdjustment, On: voidDate}, nil
	}
	return VoidTax{Kind: VoidTaxNets, On: voidDate}, nil
}

// PeriodCrossWarnings returns W-PERIOD-CROSS for each bill approved on or before periodEnd
// and still undelivered at the end of that business day (America/Toronto).
func PeriodCrossWarnings(bills []Bill, periodEnd time.Time) ([]PeriodWarning, error) {
	end := periodDate(periodEnd)
	var out []PeriodWarning
	for _, b := range bills {
		switch b.Status {
		case StatusDraft, StatusPreflightFailed, StatusPendingApproval, StatusVoid:
			continue
		}
		if b.IssueDate.IsZero() || periodDate(b.IssueDate).After(end) {
			continue
		}
		// rule: BIL-C-123
		// An approved bill still undelivered at period end carries its tax into a period
		// that may be filed before the bill is delivered or voided.
		on, err := periodDeliveredOn(b)
		if err != nil {
			return nil, err
		}
		if on.IsZero() || on.After(end) {
			out = append(out, PeriodWarning{Code: WarnPeriodCross, Bill: b.ID, Number: b.Number})
		}
	}
	return out, nil
}

// periodDate returns the calendar date of business date t at midnight UTC, so dates compare
// by day only. Business dates are already America/Toronto dates and are not converted.
func periodDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// periodDeliveredOn returns the business date b was delivered on, or zero if undelivered.
// DeliveredOn is used when set; otherwise DeliveredAt is converted to America/Toronto.
func periodDeliveredOn(b Bill) (time.Time, error) {
	if !b.DeliveredOn.IsZero() {
		return periodDate(b.DeliveredOn), nil
	}
	if b.DeliveredAt.IsZero() {
		return time.Time{}, nil
	}
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		return time.Time{}, fmt.Errorf("delivery date of bill %s: %w", b.ID, err)
	}
	return periodDate(b.DeliveredAt.In(loc)), nil
}
