package bill

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
)

// FlagBillingRecordsVerified turns the fees book export on. It is the same name as
// legal.FlagBillingRecordsVerified; app/legal imports this package, so the name is repeated.
const FlagBillingRecordsVerified = "billing.records_verified"

// ErrUnverified is returned by ExportFeesBook while the billing.records_verified flag is
// off. The export fails closed until a lawyer confirms the By-law 9 record rule.
var ErrUnverified = errors.New("bill: billing record rules not verified")

// FeesBookStatus is the state of a fees book row.
type FeesBookStatus string

// Fees book row states. A voided bill keeps its row and number.
const (
	FeesBookIssued FeesBookStatus = "issued"
	FeesBookVoid   FeesBookStatus = "void"
)

// FeesBookEntry is one row of the fees book: a numbered bill, never deleted. It holds ids
// only, never a client name, so it is safe to log. Amounts are int64 cents.
type FeesBookEntry struct {
	Number             string
	BillDate           time.Time // the bill's issue date, midnight America/Toronto
	ClientID           string    // the payer contact id
	MatterID           string
	FeesCents          int64
	DisbursementsCents int64 // taxable and non-taxable disbursements
	DiscountCents      int64
	TaxCents           int64
	TotalCents         int64
	Status             FeesBookStatus
}

// NewFeesBookEntry builds the fees book row for a numbered bill. A bill that was never
// approved (no number, approval or bill date) returns ErrNotApproved.
//
// rule: BIL-L-040 (verify)
func NewFeesBookEntry(b Bill) (FeesBookEntry, error) {
	if b.Number == "" || b.ApprovalID == "" || b.IssueDate.IsZero() || editable(b.Status) {
		return FeesBookEntry{}, fmt.Errorf("fees book row for %s: %w", b.ID, ErrNotApproved)
	}
	if _, _, _, err := ParseNumber(b.Number); err != nil {
		return FeesBookEntry{}, fmt.Errorf("fees book row for %s: %w", b.ID, err)
	}
	st := FeesBookIssued
	if b.Status == StatusVoid {
		st = FeesBookVoid
	}
	return FeesBookEntry{
		Number:             b.Number,
		BillDate:           b.IssueDate,
		ClientID:           b.PayerID,
		MatterID:           b.MatterID,
		FeesCents:          b.FeesCents,
		DisbursementsCents: b.TaxableDisbCents + b.NontaxableDisbCents,
		DiscountCents:      b.DiscountCents,
		TaxCents:           b.TaxCents,
		TotalCents:         b.TotalCents,
		Status:             st,
	}, nil
}

// ExportFeesBook returns the rows whose bill date falls in [from, to], business dates in
// America/Toronto inclusive, in number order. Void rows are kept, so the numbers run
// without a gap. With billing.records_verified off it returns ErrUnverified.
//
// rule: BIL-L-040 (verify)
func ExportFeesBook(ctx context.Context, entries []FeesBookEntry, from, to time.Time) ([]FeesBookEntry, error) {
	// rule: BIL-L-040 (verify)
	if !flags.Enabled(ctx, FlagBillingRecordsVerified) {
		return nil, fmt.Errorf("export fees book: %w", ErrUnverified)
	}
	lo, hi := businessDate(from), businessDate(to)
	if hi.Before(lo) {
		return nil, fmt.Errorf("export fees book: to before from: %w", ErrInvalid)
	}
	type keyed struct {
		e      FeesBookEntry
		prefix string
		year   int
		seq    int
	}
	var rows []keyed
	for _, e := range entries {
		d := businessDate(e.BillDate)
		if d.Before(lo) || d.After(hi) {
			continue
		}
		p, y, s, err := ParseNumber(e.Number)
		if err != nil {
			return nil, fmt.Errorf("export fees book: %w", err)
		}
		rows = append(rows, keyed{e, p, y, s})
	}
	// Numbers sort by series, year and sequence, so 10000 follows 9999.
	slices.SortFunc(rows, func(a, b keyed) int {
		return cmp.Or(cmp.Compare(a.prefix, b.prefix), cmp.Compare(a.year, b.year), cmp.Compare(a.seq, b.seq))
	})
	out := make([]FeesBookEntry, len(rows))
	for i, r := range rows {
		out[i] = r.e
	}
	return out, nil
}
