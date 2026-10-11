package reports

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
)

// FlagBillingRecordsVerified gates the fees book and retention checks below, which rest on
// unconfirmed By-Law 9 text. It is off by default. It is legalflags.BillingRecordsVerified.
const FlagBillingRecordsVerified = legalflags.BillingRecordsVerified

var (
	// ErrUnverified is returned while FlagBillingRecordsVerified is off.
	ErrUnverified = errors.New("reports: by-law 9 records rules not verified")
	// ErrRetained is returned when a record is still inside its retention period.
	ErrRetained = errors.New("reports: record inside retention period")
)

// DocKind says whether a tax document is a bill or a credit note.
type DocKind string

// Tax document kinds.
const (
	DocBill   DocKind = "bill"
	DocCredit DocKind = "credit_note"
)

// TaxDoc is one approved bill or credit note as the HST report reads it. TaxCents is the
// document's total tax, positive for both kinds; the kind sets the sign.
type TaxDoc struct {
	Kind     DocKind
	Number   string
	Day      time.Time // business date the document is dated
	TaxCents int64
	Void     bool
	Reason   string // void reason
}

// TaxDocs lists approved bills and credit notes dated in p. The billing modules implement it.
type TaxDocs interface {
	TaxDocs(ctx context.Context, p Period) ([]TaxDoc, error)
}

// HSTFrom builds the HST figures for p from src.
func HSTFrom(ctx context.Context, src TaxDocs, p Period) (HST, error) {
	if p.To.Format(time.DateOnly) < p.From.Format(time.DateOnly) {
		return HST{}, fmt.Errorf("hst %s..%s: %w", p.From.Format(time.DateOnly), p.To.Format(time.DateOnly), ErrInvalidPeriod)
	}
	docs, err := src.TaxDocs(ctx, p)
	if err != nil {
		return HST{}, fmt.Errorf("hst: tax docs: %w", err)
	}
	return SumHST(p, docs), nil
}

// SumHST sums the tax on bills minus the tax on credit notes dated in p. Documents outside p
// and voided documents are skipped.
func SumHST(p Period, docs []TaxDoc) HST {
	r := HST{Period: p}
	for _, d := range docs {
		if d.Void || !p.Contains(d.Day) {
			continue
		}
		// rule: BIL-T-090
		switch d.Kind {
		case DocBill:
			r.BillTaxCents += d.TaxCents
			r.BillCount++
		case DocCredit:
			r.CreditTaxCents += d.TaxCents
			r.CreditNoteCount++
		}
	}
	r.NetTaxCents = r.BillTaxCents - r.CreditTaxCents
	return r
}

// FeesBookLine is one bill on the fees book.
type FeesBookLine struct {
	Number   string
	Day      time.Time
	TaxCents int64
	Void     bool
	Reason   string
}

// FeesBook lists every bill dated in p, voids included, in bill number order.
func FeesBook(ctx context.Context, p Period, docs []TaxDoc) ([]FeesBookLine, error) {
	if !legalflags.Enabled(ctx, FlagBillingRecordsVerified) {
		return nil, ErrUnverified
	}
	var out []FeesBookLine
	for _, d := range docs {
		// rule: BIL-L-040 (verify)
		if d.Kind != DocBill || !p.Contains(d.Day) {
			continue
		}
		out = append(out, FeesBookLine{Number: d.Number, Day: d.Day, TaxCents: d.TaxCents, Void: d.Void, Reason: d.Reason})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

// RetainUntil is the last day a billing record dated day must be kept: six years after the
// fiscal year end that contains it. fyeMonth and fyeDay give the firm's fiscal year end.
func RetainUntil(day time.Time, fyeMonth time.Month, fyeDay int) time.Time {
	fye := time.Date(day.Year(), fyeMonth, fyeDay, 0, 0, 0, 0, day.Location())
	if day.Format(time.DateOnly) > fye.Format(time.DateOnly) {
		fye = fye.AddDate(1, 0, 0)
	}
	return fye.AddDate(6, 0, 0)
}

// CheckPurge refuses a purge of a billing record dated day on today while it is retained.
func CheckPurge(ctx context.Context, day, today time.Time, fyeMonth time.Month, fyeDay int) error {
	if !legalflags.Enabled(ctx, FlagBillingRecordsVerified) {
		return ErrUnverified
	}
	// rule: BIL-L-041 (verify)
	end := RetainUntil(day, fyeMonth, fyeDay)
	if today.Format(time.DateOnly) <= end.Format(time.DateOnly) {
		return fmt.Errorf("purge record dated %s: retained until %s: %w", day.Format(time.DateOnly), end.Format(time.DateOnly), ErrRetained)
	}
	return nil
}
