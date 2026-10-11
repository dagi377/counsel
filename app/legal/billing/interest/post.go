package interest

import (
	"context"
	"fmt"
	"time"
)

var _ Poster = (*Posting)(nil)

// ChargeStore keeps interest charges, one per bill per period start.
type ChargeStore interface {
	// Add records c. It returns ErrAlreadyPosted when the bill already has a charge with the
	// same period start.
	Add(ctx context.Context, c Charge) error
}

// TaxExempt is the tax code an interest line carries.
const TaxExempt = "EXEMPT"

// Line is the bill line an interest charge prints as. It is its own line, never merged into
// fees, and carries no tax.
type Line struct {
	BillID      string
	Description string
	AmountCents int64
	TaxCode     string
	TaxCents    int64
}

// Posting is the interest posting job: it computes one period's simple interest on a bill
// and records it as a charge.
type Posting struct {
	Start Start
	Calc  Calculator
	Store ChargeStore
}

// Post computes and records the interest charge for b over the half-open period p. Days
// before the accrual start are clipped off. A bill without an agreed rate never accrues
// interest (ErrNoAgreedRate); a period wholly before the start returns ErrNotDue.
func (s *Posting) Post(ctx context.Context, b Bill, p Period) (Charge, error) {
	// rule: BIL-L-010 (verify)
	// Interest runs at the rate the client agreed to; with no agreed rate it runs only at the
	// CJA prejudgment rate, and only while FlagCJADefaultRate is on.
	rate, err := EffectiveRate(ctx, b)
	if err != nil {
		return Charge{}, fmt.Errorf("post interest: %w", err)
	}
	b.RatePPM = rate
	clipped, err := s.Start.Clip(ctx, b, time.Time{}, p)
	if err != nil {
		return Charge{}, fmt.Errorf("post interest: %w", err)
	}
	days := daysBetween(clipped.From, clipped.To)
	amount, err := s.Calc.Simple(b.Principal, b.RatePPM, days)
	if err != nil {
		return Charge{}, fmt.Errorf("post interest %s: %w", b.ID, err)
	}
	c := Charge{BillID: b.ID, Period: clipped, Principal: b.Principal, RatePPM: b.RatePPM, Days: days, Amount: amount}
	if err := s.Store.Add(ctx, c); err != nil {
		return Charge{}, fmt.Errorf("post interest %s: %w", b.ID, err)
	}
	return c, nil
}

// ChargeLine returns the bill line for c.
//
// rule: BIL-C-204
// rule: BIL-L-049
// Interest for late payment carries no GST/HST and sits on its own non-taxable line.
func ChargeLine(c Charge) Line {
	whole, frac := int64(c.RatePPM)/10000, int64(c.RatePPM)%10000/100
	return Line{
		BillID:      c.BillID,
		Description: fmt.Sprintf("Interest at %d.%02d%% p.a. (Solicitors Act s.33)", whole, frac),
		AmountCents: int64(c.Amount),
		TaxCode:     TaxExempt,
		TaxCents:    0,
	}
}

// daysBetween counts calendar days in the half-open range [from, to) of business dates.
func daysBetween(from, to time.Time) int {
	y1, m1, d1 := from.Date()
	y2, m2, d2 := to.Date()
	a := time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC)
	b := time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}
