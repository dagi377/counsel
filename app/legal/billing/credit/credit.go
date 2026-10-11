package credit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/money"
	"github.com/dagi377/counsel/kernel/server"
)

// CodeCreditExceedsBill is the problem code for a credit larger than what is left in a
// bucket of the source bill (docs/QUESTIONS.md Q-005).
const CodeCreditExceedsBill = "credit_exceeds_bill"

// exceedsBill is the 409 problem CheckBucket wraps next to ErrExceedsBalance.
//
// rule: BIL-C-082
func exceedsBill() *api.Problem {
	p := server.NewProblem(http.StatusConflict, CodeCreditExceedsBill, "Credit exceeds what is left on the bill")
	p.Rule = "BIL-C-082"
	return &p
}

// Kind is why a credit note is issued.
type Kind string

// Credit note kinds.
const (
	KindFeeReduction    Kind = "fee_reduction"
	KindBillingError    Kind = "billing_error"
	KindBadDebtWriteOff Kind = "bad_debt_write_off"
	KindRefund          Kind = "refund"
)

// Valid reports whether k is one of the four credit note kinds.
//
// rule: BIL-C-080
func (k Kind) Valid() bool {
	switch k {
	case KindFeeReduction, KindBillingError, KindBadDebtWriteOff, KindRefund:
		return true
	}
	return false
}

// Bucket is one pre-tax bucket of the source bill (fees, disbursements, ...) with what has
// already been taken off it.
type Bucket struct {
	Key           string
	BilledCents   int64 // pre-tax billed on the source bill
	CreditedCents int64 // pre-tax already credited by earlier notes
	WrittenOff    int64 // pre-tax already written off
}

// Remaining is the pre-tax still creditable in the bucket.
func (b Bucket) Remaining() int64 { return b.BilledCents - b.CreditedCents - b.WrittenOff }

// TaxLine is one tax line of the source bill, frozen at issue. Credits always use these
// amounts, never today's rate.
type TaxLine struct {
	ID            string
	RatePPM       money.PPM // rate on the source bill; informational only
	TaxCents      int64     // tax charged on the source bill
	BaseCents     int64     // taxable base on the source bill
	CreditedBase  int64     // pre-tax already credited against this line
	CreditedCents int64     // tax already credited against this line
}

// Calc holds the pure credit-note arithmetic. Every division goes through M.Rhu.
type Calc struct {
	M money.Arithmetic // nil means money.Default
}

// CheckDelivered rejects a credit on a bill that is not yet delivered: before delivery the
// bill is edited or voided, which recomputes its tax.
//
// rule: BIL-T-071
func CheckDelivered(b Bill) error {
	if !b.Delivered {
		return fmt.Errorf("credit bill %s: %w", b.ID, ErrBillNotDelivered)
	}
	return nil
}

// CheckBucket rejects a pre-tax credit larger than what is left in the bucket.
//
// rule: BIL-C-082
// rule: BIL-I-014
func CheckBucket(b Bucket, creditPreTax int64) error {
	if creditPreTax <= 0 {
		return fmt.Errorf("credit bucket %s: %w", b.Key, ErrInvalidAmount)
	}
	if creditPreTax > b.Remaining() {
		return fmt.Errorf("credit bucket %s: %w: %w", b.Key, ErrExceedsBalance, exceedsBill())
	}
	return nil
}

// LineTax returns the tax reversed on one source tax line for a pre-tax credit:
// rhu(credit * line_tax, line_base). A credit that exhausts the remaining base takes exactly
// the remaining tax, and no credit takes more than the remaining tax.
//
// rule: BIL-C-203
// rule: BIL-T-070
func (c Calc) LineTax(l TaxLine, creditPreTax int64) (int64, error) {
	remBase := l.BaseCents - l.CreditedBase
	remTax := l.TaxCents - l.CreditedCents
	if creditPreTax <= 0 || l.BaseCents <= 0 {
		return 0, fmt.Errorf("credit tax line %s: %w", l.ID, ErrInvalidAmount)
	}
	if creditPreTax > remBase {
		return 0, fmt.Errorf("credit tax line %s: %w", l.ID, ErrExceedsBalance)
	}
	if creditPreTax == remBase {
		return remTax, nil
	}
	tax, err := c.ratio(creditPreTax, l.TaxCents, l.BaseCents)
	if err != nil {
		return 0, fmt.Errorf("credit tax line %s: %w", l.ID, err)
	}
	return min(tax, remTax), nil
}

// Route is where an approved credit goes.
type Route struct {
	ARCents     int64 // reduces the bill's open receivable
	TrustCents  int64 // repaid to trust (trust-sourced allocations first)
	ClientCents int64 // left as client credit
}

// FlagCreditRouteVerified gates routing a credit beyond the bill's open AR (back to trust,
// then client credit). It mirrors legal.FlagCreditRouteVerified, which this package cannot
// import. Off by default.
const FlagCreditRouteVerified = "billing.credit_route_verified"

// ErrUnverified is returned when a credit must be routed past the bill's open AR while
// FlagCreditRouteVerified is off: the excess is refused, never silently routed.
var ErrUnverified = errors.New("credit: credit routing rule not verified")

// RouteCredit splits a credit: first the bill's open AR, then back to trust up to the
// trust-sourced payments, then client credit. While FlagCreditRouteVerified is off only a
// credit that fits within the open AR is routed; any excess returns ErrUnverified.
//
// rule: BIL-C-081 (verify)
func RouteCredit(ctx context.Context, totalCents, openARCents, trustPaidCents int64) (Route, error) {
	if totalCents <= 0 || openARCents < 0 || trustPaidCents < 0 {
		return Route{}, fmt.Errorf("route credit: %w", ErrInvalidAmount)
	}
	var r Route
	r.ARCents = min(totalCents, openARCents)
	left := totalCents - r.ARCents
	// rule: BIL-C-081 (verify)
	if left > 0 && !flags.Enabled(ctx, FlagCreditRouteVerified) {
		return Route{}, fmt.Errorf("route credit past open AR: %w", ErrUnverified)
	}
	r.TrustCents = min(left, trustPaidCents)
	r.ClientCents = left - r.TrustCents
	return r, nil
}

// BadDebtTax is the tax adjustment on writing off part of a bill:
// rhu(bill_tax * written_off, bill_total_incl_tax).
//
// rule: BIL-C-085
// rule: BIL-T-080
func (c Calc) BadDebtTax(writtenOff, billTax, billTotal int64) (int64, error) {
	if writtenOff <= 0 || billTotal <= 0 || billTax < 0 {
		return 0, fmt.Errorf("bad debt tax: %w", ErrInvalidAmount)
	}
	if writtenOff > billTotal {
		return 0, fmt.Errorf("bad debt tax: %w", ErrExceedsBalance)
	}
	t, err := c.ratio(writtenOff, billTax, billTotal)
	if err != nil {
		return 0, fmt.Errorf("bad debt tax: %w", err)
	}
	return t, nil
}

// Recovery splits a payment received on a written-off bill.
type Recovery struct {
	TaxCents    int64 // tax re-included in the recovery period
	PreTaxCents int64 // credited back against bad debt
}

// RecoverAfterWriteOff splits a recovered amount, capped at what is still written off,
// into tax rhu(recovered * bill_tax, bill_total) and the pre-tax rest.
//
// rule: BIL-C-086
// rule: BIL-T-081
func (c Calc) RecoverAfterWriteOff(recovered, writtenOffLeft, billTax, billTotal int64) (Recovery, error) {
	if recovered <= 0 || billTotal <= 0 || billTax < 0 {
		return Recovery{}, fmt.Errorf("recovery: %w", ErrInvalidAmount)
	}
	if recovered > writtenOffLeft {
		return Recovery{}, fmt.Errorf("recovery: %w", ErrExceedsBalance)
	}
	t, err := c.ratio(recovered, billTax, billTotal)
	if err != nil {
		return Recovery{}, fmt.Errorf("recovery: %w", err)
	}
	return Recovery{TaxCents: t, PreTaxCents: recovered - t}, nil
}

// ratio returns rhu(a*b, den) without wrapping.
func (c Calc) ratio(a, b, den int64) (int64, error) {
	if a != 0 && b != 0 && (a > math.MaxInt64/b || a < 0 || b < 0) {
		return 0, money.ErrOverflow
	}
	return c.arith().Rhu(a*b, den)
}

// arith returns M, or money.Default when M is nil.
func (c Calc) arith() money.Arithmetic {
	if c.M == nil {
		return money.Default
	}
	return c.M
}
