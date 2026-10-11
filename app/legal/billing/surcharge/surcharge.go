package surcharge

import (
	"context"
	"errors"
	"fmt"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/kernel/money"
)

// Flag gates card surcharges (V-SURCHARGE). It is legalflags.CardSurcharge. Off by default.
const Flag = legalflags.CardSurcharge

// CapPPM is the card-network ceiling on a surcharge rate: 2.40%.
const CapPPM int64 = 24000

const ppmDenominator int64 = 1_000_000

var (
	// ErrIneligible means a surcharge was asked for a payment that fails BIL-C-100.
	ErrIneligible = errors.New("surcharge: payment not eligible")
	// ErrTrust means a surcharge was routed to the trust book.
	ErrTrust = errors.New("surcharge: never posts to trust")
	// ErrInvalid means a negative amount, rate or a refund larger than the payment.
	ErrInvalid = errors.New("surcharge: invalid input")
)

// Funding is the funding source of the card.
type Funding string

// Card funding sources. Only credit may be surcharged.
const (
	FundingCredit  Funding = "credit"
	FundingDebit   Funding = "debit"
	FundingPrepaid Funding = "prepaid"
	FundingWallet  Funding = "wallet"
)

// Book is the ledger book a payment lands in.
type Book string

// Books.
const (
	BookOperating Book = "operating"
	BookTrust     Book = "trust"
)

// Snapshot is every BIL-C-100 condition as it stood at capture time. It is stored with the
// payment so the surcharge can be checked later (BIL-I-022).
type Snapshot struct {
	FirmSettingOn          bool
	NoticeDatePassed       bool
	SurchargeFreeOffered   bool
	Funding                Funding
	Destination            Book
	FirmInQuebec           bool
	BillingAddressInQuebec bool
	LSOCheckAttached       bool
}

// TaxLine is one tax line of the paid bill, or the tax on a surcharge at that line's code.
type TaxLine struct {
	Code  string
	Cents int64
}

// Charge is a computed surcharge and its tax lines.
type Charge struct {
	RatePPM    int64
	Cents      int64
	Tax        []TaxLine
	TotalCents int64 // Cents plus every tax line
}

// Calc computes card surcharges. Money does every rounded division.
type Calc struct {
	Money money.Arithmetic
}

// Eligible reports whether every BIL-C-100 condition holds and the feature flag is on.
//
// rule: BIL-C-100
func (c Calc) Eligible(ctx context.Context, s Snapshot) bool {
	return legalflags.Enabled(ctx, Flag) &&
		s.FirmSettingOn && s.NoticeDatePassed && s.SurchargeFreeOffered &&
		s.Funding == FundingCredit &&
		// rule: BIL-I-015
		s.Destination == BookOperating &&
		!s.FirmInQuebec && !s.BillingAddressInQuebec &&
		s.LSOCheckAttached
}

// Rate caps the firm's configured rate at min(24000, cost_of_acceptance_ppm).
//
// rule: BIL-C-101
func Rate(configuredPPM, costPPM int64) int64 {
	r := min(configuredPPM, CapPPM, costPPM)
	return max(r, 0)
}

// Compute returns the surcharge on appliedCents and its tax. An ineligible payment gets a
// zero charge. billTotal and billTax are the paid bill's total and tax; billTaxLines its
// tax lines.
func (c Calc) Compute(ctx context.Context, s Snapshot, appliedCents, configuredPPM, costPPM, billTotal, billTax int64, billTaxLines []TaxLine) (Charge, error) {
	if appliedCents < 0 || configuredPPM < 0 || costPPM < 0 {
		return Charge{}, fmt.Errorf("surcharge compute: %w", ErrInvalid)
	}
	// rule: BIL-C-100
	if !c.Eligible(ctx, s) {
		return Charge{}, nil
	}
	ppm := Rate(configuredPPM, costPPM)
	// rule: BIL-C-101
	cents, err := c.Money.Rhu(appliedCents*ppm, ppmDenominator)
	if err != nil {
		return Charge{}, fmt.Errorf("surcharge amount: %w", err)
	}
	tax, err := c.Tax(cents, billTotal, billTax, billTaxLines)
	if err != nil {
		return Charge{}, err
	}
	ch := Charge{RatePPM: ppm, Cents: cents, Tax: tax, TotalCents: cents}
	for _, t := range tax {
		ch.TotalCents += t.Cents
	}
	return ch, nil
}

// Tax taxes a surcharge at the paid bill's tax lines: per line,
// rhu(surcharge * line_tax, bill_total - bill_tax).
//
// rule: BIL-C-208
// rule: BIL-T-044
func (c Calc) Tax(surchargeCents, billTotal, billTax int64, billTaxLines []TaxLine) ([]TaxLine, error) {
	pre := billTotal - billTax
	if pre <= 0 {
		return nil, fmt.Errorf("surcharge tax: bill pre-tax total %d: %w", pre, ErrInvalid)
	}
	out := make([]TaxLine, 0, len(billTaxLines))
	for _, l := range billTaxLines {
		v, err := c.Money.Rhu(surchargeCents*l.Cents, pre)
		if err != nil {
			return nil, fmt.Errorf("surcharge tax %s: %w", l.Code, err)
		}
		out = append(out, TaxLine{Code: l.Code, Cents: v})
	}
	return out, nil
}

// Refund returns the surcharge and tax refunded when refunded of appliedCents is refunded:
// rhu(x * refunded, applied) for the surcharge and each tax line. A full refund returns
// the charge exactly.
//
// rule: BIL-C-102
func (c Calc) Refund(ch Charge, refunded, appliedCents int64) (Charge, error) {
	if refunded < 0 || appliedCents <= 0 || refunded > appliedCents {
		return Charge{}, fmt.Errorf("surcharge refund %d of %d: %w", refunded, appliedCents, ErrInvalid)
	}
	if refunded == appliedCents {
		return ch, nil
	}
	cents, err := c.Money.Rhu(ch.Cents*refunded, appliedCents)
	if err != nil {
		return Charge{}, fmt.Errorf("surcharge refund: %w", err)
	}
	out := Charge{RatePPM: ch.RatePPM, Cents: cents, TotalCents: cents}
	for _, t := range ch.Tax {
		v, err := c.Money.Rhu(t.Cents*refunded, appliedCents)
		if err != nil {
			return Charge{}, fmt.Errorf("surcharge refund tax %s: %w", t.Code, err)
		}
		out.Tax = append(out.Tax, TaxLine{Code: t.Code, Cents: v})
		out.TotalCents += v
	}
	return out, nil
}

// Check refuses a stored surcharge that should not exist: a surcharge > 0 needs every
// BIL-C-100 condition in the capture snapshot, and a surcharge never posts to trust.
//
// rule: BIL-I-022
func (c Calc) Check(ctx context.Context, s Snapshot, surchargeCents int64) error {
	if surchargeCents <= 0 {
		return nil
	}
	// rule: BIL-I-015
	if s.Destination == BookTrust {
		return ErrTrust
	}
	if !c.Eligible(ctx, s) {
		return ErrIneligible
	}
	return nil
}
