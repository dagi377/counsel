package clio

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/kernel/money"
)

// FlagBylaw9Verified gates bill history import until the trust rules are confirmed (V-TRUST).
// It is legalflags.TrustBylaw9Verified.
const FlagBylaw9Verified = legalflags.TrustBylaw9Verified

var (
	// ErrBillImportDisabled is returned while FlagBylaw9Verified is off.
	ErrBillImportDisabled = errors.New("clio: bill import is disabled")
	// ErrBadBill is returned for a bill with no source id or negative amounts.
	ErrBadBill = errors.New("clio: bad bill")
	// ErrHistoryReadOnly is returned for any edit or delete of an imported bill (MIG-034).
	ErrHistoryReadOnly = errors.New("clio: imported history is read-only")
)

// SourceBillLine is one taxed line of a source bill, in cents. RatePPM is the tax rate the
// source applied (130000 = 13%), used only to check TaxCents.
type SourceBillLine struct {
	TaxCode   string
	BaseCents int64
	RatePPM   int64
	TaxCents  int64
}

// SourceBill is a bill as read from the source, already converted to cents.
type SourceBill struct {
	SourceID      string
	SubtotalCents int64
	Lines         []SourceBillLine
}

// ImportedBill is a historical bill as stored: the source's tax amounts, unchanged, and a
// warning per line whose tax differs from base x rate.
type ImportedBill struct {
	SourceID      string
	SubtotalCents int64
	TaxCents      int64
	TotalCents    int64
	Lines         []SourceBillLine
	Warnings      []string
	Historical    bool
}

// ImportBill turns a source bill into an imported historical bill. It never recomputes tax:
// a difference from base x rate is a warning, not a correction.
func ImportBill(ctx context.Context, b SourceBill) (ImportedBill, error) {
	// rule: MIG-023 (verify)
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return ImportedBill{}, ErrBillImportDisabled
	}
	if b.SourceID == "" || b.SubtotalCents < 0 {
		return ImportedBill{}, fmt.Errorf("import bill: %w", ErrBadBill)
	}
	out := ImportedBill{
		SourceID:      b.SourceID,
		SubtotalCents: b.SubtotalCents,
		Lines:         append([]SourceBillLine(nil), b.Lines...),
		Historical:    true,
	}
	for i, l := range b.Lines {
		if l.BaseCents < 0 || l.TaxCents < 0 || l.RatePPM < 0 {
			return ImportedBill{}, fmt.Errorf("import bill %s line %d: %w", b.SourceID, i, ErrBadBill)
		}
		// rule: MIG-023
		out.TaxCents += l.TaxCents
		want, err := money.MulRhu(l.BaseCents, l.RatePPM, money.PPMScale)
		if err != nil {
			return ImportedBill{}, fmt.Errorf("import bill %s line %d: check tax: %w", b.SourceID, i, err)
		}
		if d := l.TaxCents - want; d != 0 {
			out.Warnings = append(out.Warnings, taxWarning(l.RatePPM, d))
		}
	}
	out.TotalCents = out.SubtotalCents + out.TaxCents
	return out, nil
}

// CheckBillMutation rejects every edit or delete of an imported bill; a correction is a new
// live row through the normal gates.
func CheckBillMutation(b ImportedBill) error {
	// rule: MIG-034
	if b.Historical {
		return fmt.Errorf("bill %s: %w", b.SourceID, ErrHistoryReadOnly)
	}
	return nil
}

// taxWarning reads "tax differs from 13% by 1 cent".
func taxWarning(ratePPM, diffCents int64) string {
	if diffCents < 0 {
		diffCents = -diffCents
	}
	unit := "cents"
	if diffCents == 1 {
		unit = "cent"
	}
	return fmt.Sprintf("tax differs from %s by %d %s", percent(ratePPM), diffCents, unit)
}

// percent formats a ppm rate as a percentage without trailing zeros: 130000 -> "13%",
// 45000 -> "4.5%".
func percent(ppm int64) string {
	whole, frac := ppm/10000, ppm%10000
	if frac == 0 {
		return strconv.FormatInt(whole, 10) + "%"
	}
	f := fmt.Sprintf("%04d", frac)
	for f[len(f)-1] == '0' {
		f = f[:len(f)-1]
	}
	return fmt.Sprintf("%d.%s%%", whole, f)
}
