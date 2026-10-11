// CONTRACT: app/legal/billing/total. Frozen once on main; changes are additive only and
// need a docs/QUESTIONS.md entry. Bodies live in the T-018 subtasks (tax.go, total.go,
// taxpoint.go). Source: spec/eng/b3-billing-engine.json (BIL-C-002, BIL-C-200, BIL-I-004),
// spec/eng/b2-sales-tax.json (BIL-T-040..043, BIL-T-050).
//
// Money is int64 cents and rates are int64 ppm. Totals are always recomputed on the
// server; totals a client sends are ignored.

package total

import (
	"errors"
	"time"
)

// Line is one bill line after discounts, as the total needs it.
type Line struct {
	ID       string
	NetCents int64    // line net after discounts and markups, already rounded
	TaxCodes []string // tax lines this line is taxable under; empty means exempt
}

// Rate is one tax line's rate effective on the tax point.
type Rate struct {
	Code    string // e.g. "HST-ON"
	RatePPM int64
}

// Draft is what Compute needs: the lines and the rates effective on the tax point.
type Draft struct {
	Lines []Line
	Rates []Rate
}

// TaxLine is the tax for one tax code, once per bill on the summed taxable base.
type TaxLine struct {
	Code      string
	RatePPM   int64
	BaseCents int64 // sum of taxable line nets
	TaxCents  int64 // rhu(BaseCents * RatePPM, 1e6)
}

// Totals is a bill's computed totals. TotalCents = NetCents + TaxCents (BIL-I-004).
type Totals struct {
	NetCents   int64
	Tax        []TaxLine // ordered by Code
	TaxCents   int64
	TotalCents int64
}

// TaxPointInput holds the dates the tax point depends on. Dates are calendar days in
// America/Toronto; zero means "not yet".
type TaxPointInput struct {
	ApprovedOn time.Time // the bill date: consideration is due no later than this
	DueOn      time.Time // contractual due date, if earlier than the bill date
	PaidOn     time.Time // first payment received
}

// Sentinel errors.
var (
	ErrUnknownTaxCode = errors.New("total: line uses a tax code with no rate")
	ErrDuplicateRate  = errors.New("total: tax code has two rates")
	ErrNegativeRate   = errors.New("total: negative tax rate")
	ErrNoTaxPoint     = errors.New("total: bill neither approved nor paid")
)

// Calculator computes bill totals (T-018.1, T-018.2) and the tax point (T-018.3).
type Calculator interface {
	// Compute returns the totals of a draft. Tax is computed once per tax code on the
	// summed base, never per line.
	Compute(d Draft) (Totals, error)
	// TaxPoint returns the day tax becomes payable: the earlier of paid and due, where
	// due is no later than the bill date.
	TaxPoint(in TaxPointInput) (time.Time, error)
}
