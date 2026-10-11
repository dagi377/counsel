package draft

import (
	"errors"
	"fmt"
	"sort"

	"github.com/dagi377/counsel/kernel/money"
)

// ErrOutOfScope means out-of-scope work on a pure flat arrangement is blocked from the bill
// until a change-of-scope letter is approved (BIL-C-031).
var ErrOutOfScope = errors.New("draft: out-of-scope work blocked on flat arrangement")

// LineNet returns a line's net: AmountCents - Reduction - LineDiscountCents -
// DiscountShareCents. Tax is computed on it.
func LineNet(l Line) int64 {
	n := l.AmountCents - l.LineDiscountCents - l.DiscountShareCents
	if l.Reduction != nil {
		n -= l.Reduction.Cents
	}
	return n
}

// discountable says whether a bill discount on base may reach l.
// rule: BIL-T-042
// rule: BIL-T-032
// A whole-bill discount reaches fee and own-cost lines only, never agent disbursements.
func discountable(l Line, d Discount) bool {
	switch d.Base {
	case BaseFeesOnly:
		return l.Kind == LineFee || l.Kind == LineFlat
	case BaseFeesAndOwn:
		return l.Kind == LineFee || l.Kind == LineFlat || l.Kind == LineOwnCost
	case BaseOneLine:
		return l.ID == d.LineID && l.Kind != LineAgentDis
	}
	return false
}

// SpreadDiscount spreads a bill discount of totalCents over the lines d.Base selects, by
// largest remainder on weight = line net; ties go to the earliest SortOrder. It returns a
// copy of lines with DiscountShareCents set; the input is not changed.
// rule: BIL-C-022
func SpreadDiscount(a money.Arithmetic, lines []Line, d Discount, totalCents int64) ([]Line, error) {
	out := append([]Line(nil), lines...)
	idx := make([]int, 0, len(out))
	for i, l := range out {
		if discountable(l, d) {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 || totalCents < 0 {
		return nil, fmt.Errorf("spread discount %d over %s: %w", totalCents, d.Base, ErrInvalidDiscount)
	}
	sort.SliceStable(idx, func(i, j int) bool { return out[idx[i]].SortOrder < out[idx[j]].SortOrder })
	weights := make([]int64, len(idx))
	var base int64
	for k, i := range idx {
		weights[k] = LineNet(out[i])
		base += weights[k]
	}
	if totalCents > base {
		return nil, fmt.Errorf("spread discount %d over base %d: %w", totalCents, base, ErrNegativeLine)
	}
	parts, err := a.Allocate(money.Cents(totalCents), weights)
	if err != nil {
		return nil, fmt.Errorf("spread discount: %w", err)
	}
	for k, i := range idx {
		out[i].DiscountShareCents += int64(parts[k])
	}
	return out, nil
}

// PercentOf returns rhu(cents * ppm, 1e6): a percent discount, a flat milestone or a markup.
func PercentOf(a money.Arithmetic, cents, ppm int64) (int64, error) {
	if ppm < 0 || cents > 0 && ppm > 0 && cents > (1<<62)/ppm {
		return 0, fmt.Errorf("percent of %d at %d ppm: %w", cents, ppm, money.ErrOverflow)
	}
	v, err := a.Rhu(cents*ppm, money.PPMScale)
	if err != nil {
		return 0, fmt.Errorf("percent of %d: %w", cents, err)
	}
	return v, nil
}

// CheckReason validates a write-down reason.
// rule: BIL-C-023
// reason in {duplicate, inefficiency, training, courtesy, over_estimate, scope, other};
// "other" needs text.
func CheckReason(r Reduction) error {
	switch r.Reason {
	case ReasonDuplicate, ReasonInefficiency, ReasonTraining, ReasonCourtesy, ReasonOverEstimate, ReasonScope:
		return nil
	case ReasonOther:
		if r.ReasonText != "" {
			return nil
		}
	}
	return fmt.Errorf("write-down reason %q: %w", r.Reason, ErrReasonRequired)
}

// FlatMilestone returns a flat fee milestone line: rhu(flat * ppm, 1e6).
// rule: BIL-C-030
func FlatMilestone(a money.Arithmetic, id string, flatCents, ppm int64, sortOrder int) (Line, error) {
	amt, err := PercentOf(a, flatCents, ppm)
	if err != nil {
		return Line{}, fmt.Errorf("flat milestone %s: %w", id, err)
	}
	return Line{ID: id, Kind: LineFlat, SortOrder: sortOrder, AmountCents: amt, Taxable: true}, nil
}

// OutOfScope says whether out-of-scope time on a flat matter may be billed hourly.
// rule: BIL-C-031
// Hybrid arrangement: billed hourly. Pure flat: blocked until a change-of-scope letter is
// approved.
func OutOfScope(hybrid, scopeChangeApproved bool) error {
	if hybrid || scopeChangeApproved {
		return nil
	}
	return ErrOutOfScope
}

// Disbursement is an expense as a draft needs it.
type Disbursement struct {
	ID               string
	Agent            bool  // paid as the client's agent (BIL-T-030)
	PreTaxCents      int64 // vendor amount before vendor tax (own costs)
	PaidCents        int64 // exact amount paid including vendor tax (agent costs)
	MarkupPPM        int64
	SortOrder        int
	Description      string
	OriginalMinor    int64  // foreign amount in minor units; 0 when CAD
	OriginalCurrency string // ISO 4217; "" when CAD
	FXPPM            int64  // CAD per unit in ppm
}

// DisbursementLine builds the bill line for one disbursement.
//
// rule: BIL-C-040
// rule: BIL-T-030
// Agent costs are outside the tax base; own costs are taxed at the matter's tax code.
//
// rule: BIL-T-031
// Own costs bill the vendor's pre-tax amount, never an amount that already includes tax.
//
// rule: BIL-T-032
// Agent costs bill the exact amount paid and are never marked up.
func DisbursementLine(a money.Arithmetic, d Disbursement) (Line, error) {
	if d.Agent {
		if d.MarkupPPM != 0 {
			return Line{}, fmt.Errorf("disbursement %s: %w", d.ID, ErrMarkupOnAgent)
		}
		return Line{ID: d.ID, Kind: LineAgentDis, SortOrder: d.SortOrder, Description: d.Description, AmountCents: d.PaidCents}, nil
	}
	cost := d.PreTaxCents
	if d.OriginalCurrency != "" {
		c, err := FXCents(a, d.OriginalMinor, d.FXPPM)
		if err != nil {
			return Line{}, fmt.Errorf("disbursement %s: %w", d.ID, err)
		}
		cost = c
	}
	mk, err := PercentOf(a, cost, d.MarkupPPM)
	if err != nil {
		return Line{}, fmt.Errorf("disbursement %s markup: %w", d.ID, err)
	}
	return Line{ID: d.ID, Kind: LineOwnCost, SortOrder: d.SortOrder, Description: d.Description, AmountCents: cost + mk, MarkupCents: mk, Taxable: true}, nil
}

// FXCents converts a foreign amount to CAD cents: rhu(original_minor * fx_ppm, 1e6).
// rule: BIL-C-043
func FXCents(a money.Arithmetic, originalMinor, fxPPM int64) (int64, error) {
	if fxPPM <= 0 {
		return 0, fmt.Errorf("fx rate %d: %w", fxPPM, ErrInvalidDiscount)
	}
	return PercentOf(a, originalMinor, fxPPM)
}

// Sections is a statement's split of fees and disbursements (BIL-L-028).
type Sections struct {
	FeesCents          int64
	TaxableDisbCents   int64
	NonTaxableDisCents int64
}

// SplitSections totals line nets into fees and disbursements.
// rule: BIL-L-028
// A statement details fees and disbursements separately; a line of any other kind (for
// example a mixed "services and expenses" line) blocks delivery.
func SplitSections(lines []Line) (Sections, error) {
	var s Sections
	for _, l := range lines {
		switch l.Kind {
		case LineFee, LineFlat:
			s.FeesCents += LineNet(l)
		case LineOwnCost:
			s.TaxableDisbCents += LineNet(l)
		case LineAgentDis:
			s.NonTaxableDisCents += LineNet(l)
		default:
			return Sections{}, fmt.Errorf("line %s kind %q MIXED_LINE: %w", l.ID, l.Kind, ErrDeliveryBlocked)
		}
	}
	return s, nil
}
