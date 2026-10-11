// CONTRACT: app/legal/reports. Exported shapes here are frozen after wave 0; changes are
// additive only and need a docs/QUESTIONS.md entry. Bodies live in the T-038L subtasks.

// Package reports builds the legal reports: the HST return figures, the monthly client
// trust listing, AR aging and work in progress. Every report reconciles to a ledger account.
package reports

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrInvalidPeriod is returned when a period's end is before its start.
	ErrInvalidPeriod = errors.New("reports: invalid period")
	// ErrNotFound is returned for an unknown trust account.
	ErrNotFound = errors.New("reports: not found")
	// ErrOutOfBalance is returned when a report does not reconcile to its ledger account.
	ErrOutOfBalance = errors.New("reports: does not reconcile to ledger")
)

// Period is an inclusive range of business dates in America/Toronto.
type Period struct {
	From time.Time
	To   time.Time
}

// Contains reports whether day falls in the period, comparing calendar dates.
func (p Period) Contains(day time.Time) bool {
	d := day.Format(time.DateOnly)
	return d >= p.From.Format(time.DateOnly) && d <= p.To.Format(time.DateOnly)
}

// HST is the sales tax figures for one period: tax on bills minus tax on credit notes.
type HST struct {
	Period          Period
	BillTaxCents    int64
	CreditTaxCents  int64
	NetTaxCents     int64 // BillTaxCents - CreditTaxCents
	BillCount       int
	CreditNoteCount int
}

// TrustLine is one client matter's balance on a trust listing.
type TrustLine struct {
	ClientID     string
	MatterID     string
	BalanceCents int64
}

// TrustListing is the monthly client trust listing for one trust account. TotalCents must
// equal LedgerCents, the account's TR-2500 balance at month end.
type TrustListing struct {
	TrustAccountID string
	MonthEnd       time.Time
	Lines          []TrustLine
	TotalCents     int64
	LedgerCents    int64
}

// AgingBucket indexes Aging.Buckets.
type AgingBucket int

// Aging buckets by days past the bill date.
const (
	Days0To30 AgingBucket = iota
	Days31To60
	Days61To90
	DaysOver90
)

// Aging is accounts receivable by age at AsOf. The buckets sum to TotalCents, which must
// equal the OP-1100 balance.
type Aging struct {
	AsOf        time.Time
	Buckets     [4]int64
	TotalCents  int64
	LedgerCents int64
}

// WIPLine is one matter's approved unbilled time at resolved rates.
type WIPLine struct {
	MatterID string
	Cents    int64
}

// WIP is work in progress at AsOf.
type WIP struct {
	AsOf       time.Time
	Lines      []WIPLine
	TotalCents int64
}

// Reports is the report surface other tasks call.
type Reports interface {
	// HST returns the sales tax figures for p.
	HST(ctx context.Context, p Period) (HST, error)
	// TrustListing returns the client trust listing for the month containing monthEnd;
	// ErrOutOfBalance (with the listing) when it does not equal TR-2500.
	TrustListing(ctx context.Context, trustAccountID string, monthEnd time.Time) (TrustListing, error)
	// Aging returns AR aging at asOf; ErrOutOfBalance when it does not equal OP-1100.
	Aging(ctx context.Context, asOf time.Time) (Aging, error)
	// WIP returns approved unbilled time at asOf, per matter, ordered by MatterID.
	WIP(ctx context.Context, asOf time.Time) (WIP, error)
}
