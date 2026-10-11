package reports

import (
	"context"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/books"
)

// OpenBill is one bill's unpaid balance at the aging date.
type OpenBill struct {
	BillID       string
	BilledOn     time.Time // business date of the bill, America/Toronto
	BalanceCents int64
}

// AgingSource reads the rows an AR aging report is built from.
type AgingSource interface {
	// OpenBills returns every bill with an unpaid balance at the end of day asOf.
	OpenBills(ctx context.Context, asOf time.Time) ([]OpenBill, error)
	// AccountBalance returns the balance of operating ledger account code at the end of day asOf.
	AccountBalance(ctx context.Context, code string, asOf time.Time) (int64, error)
}

// AgingReporter builds AR aging reports.
type AgingReporter struct {
	Source AgingSource
}

// civilDay returns day's calendar date as midnight UTC, so day counts ignore DST.
func civilDay(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
}

// daysBetween returns the calendar days from a to b.
func daysBetween(a, b time.Time) int {
	return int(civilDay(b).Sub(civilDay(a)).Hours() / 24)
}

// BucketFor returns the aging bucket for a bill daysPast days after its bill date.
func BucketFor(daysPast int) AgingBucket {
	switch {
	case daysPast <= 30:
		return Days0To30
	case daysPast <= 60:
		return Days31To60
	case daysPast <= 90:
		return Days61To90
	default:
		return DaysOver90
	}
}

// Aging returns AR aging at asOf. Bills dated after asOf are left out. The buckets sum to
// TotalCents, which is checked against the OP-1100 balance; when they differ the report is
// returned with ErrOutOfBalance.
func (r AgingReporter) Aging(ctx context.Context, asOf time.Time) (Aging, error) {
	bills, err := r.Source.OpenBills(ctx, asOf)
	if err != nil {
		return Aging{}, fmt.Errorf("aging: open bills: %w", err)
	}
	ledger, err := r.Source.AccountBalance(ctx, books.AcctARFees, asOf)
	if err != nil {
		return Aging{}, fmt.Errorf("aging: ledger balance: %w", err)
	}
	a := Aging{AsOf: asOf, LedgerCents: ledger}
	for _, b := range bills {
		days := daysBetween(b.BilledOn, asOf)
		if days < 0 {
			continue
		}
		a.Buckets[BucketFor(days)] += b.BalanceCents
		a.TotalCents += b.BalanceCents
	}
	if a.TotalCents != a.LedgerCents {
		return a, fmt.Errorf("aging: buckets %d, %s %d: %w", a.TotalCents, books.AcctARFees, a.LedgerCents, ErrOutOfBalance)
	}
	return a, nil
}
