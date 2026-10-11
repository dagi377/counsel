package draft

import (
	"fmt"

	"github.com/dagi377/counsel/kernel/money"
)

// eligibleBase returns the sum of line nets a bill discount d reaches (BIL-C-020 step 3).
func eligibleBase(lines []Line, d Discount) (int64, error) {
	var base int64
	n := 0
	for _, l := range lines {
		if !discountable(l, d) {
			continue
		}
		net := LineNet(l)
		if net < 0 {
			return 0, fmt.Errorf("discount base line %s net %d: %w", l.ID, net, ErrNegativeLine)
		}
		base += net
		n++
	}
	if n == 0 {
		return 0, fmt.Errorf("discount base %s: no eligible line: %w", d.Base, ErrInvalidDiscount)
	}
	return base, nil
}

// DiscountAmount returns the bill discount in cents on the lines d.Base selects.
// rule: BIL-C-021
// percent: rhu(eligible_base * ppm, 1e6); fixed: min(amount, eligible_base). A discount
// never makes a line negative.
// rule: BIL-C-020
// The eligible base is fee and own-cost lines (or fees only, or one line); never agent
// disbursements. It is taken after line write-downs and line discounts.
func DiscountAmount(a money.Arithmetic, lines []Line, d Discount) (int64, error) {
	switch d.Base {
	case BaseFeesAndOwn, BaseFeesOnly:
	case BaseOneLine:
		if d.LineID == "" {
			return 0, fmt.Errorf("one-line discount without line id: %w", ErrInvalidDiscount)
		}
	default:
		return 0, fmt.Errorf("discount base %q: %w", d.Base, ErrInvalidDiscount)
	}
	base, err := eligibleBase(lines, d)
	if err != nil {
		return 0, err
	}
	switch d.Kind {
	case DiscountPercent:
		if d.PPM <= 0 || d.PPM > money.PPMScale {
			return 0, fmt.Errorf("percent discount %d ppm: %w", d.PPM, ErrInvalidDiscount)
		}
		return PercentOf(a, base, d.PPM)
	case DiscountFixed:
		if d.AmountCents <= 0 {
			return 0, fmt.Errorf("fixed discount %d: %w", d.AmountCents, ErrInvalidDiscount)
		}
		return min(d.AmountCents, base), nil
	}
	return 0, fmt.Errorf("discount kind %q: %w", d.Kind, ErrInvalidDiscount)
}

// ApplyDiscount computes the bill discount and spreads it over the eligible lines by largest
// remainder on line net, ties to the earliest SortOrder. It returns a copy of lines with
// DiscountShareCents set; agent disbursements are never touched.
// rule: BIL-C-021
// rule: BIL-C-022
func ApplyDiscount(a money.Arithmetic, lines []Line, d Discount) ([]Line, error) {
	amt, err := DiscountAmount(a, lines, d)
	if err != nil {
		return nil, fmt.Errorf("apply discount: %w", err)
	}
	out, err := SpreadDiscount(a, lines, d, amt)
	if err != nil {
		return nil, fmt.Errorf("apply discount: %w", err)
	}
	for _, l := range out {
		if LineNet(l) < 0 {
			return nil, fmt.Errorf("apply discount line %s: %w", l.ID, ErrNegativeLine)
		}
	}
	return out, nil
}
