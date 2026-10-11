package total

import (
	"fmt"
	"math"
	"sort"

	"github.com/dagi377/counsel/kernel/money"
)

// TaxCodeNone marks a non-taxable line, such as late-payment interest (BIL-T-043).
const TaxCodeNone = "NONE"

// TaxLines computes the tax of a bill, one TaxLine per tax code, ordered by Code.
//
// rule: BIL-C-200
// rule: BIL-T-040
// Tax is computed once per tax line on the summed taxable base after discounts, then
// rounded once: rhu(base*rate_ppm, 1e6). Never per line.
//
// rule: BIL-T-041
// Each tax (for example GST and QST) is computed independently on the same pre-tax base;
// no tax is charged on tax.
//
// rule: BIL-T-043
// Lines with no tax code or TaxCodeNone (late-payment interest and charges) are not taxed.
// An early-payment discount is not a bill line, so it never lowers the base.
func TaxLines(a money.Arithmetic, lines []Line, rates []Rate) ([]TaxLine, error) {
	byCode := make(map[string]int64, len(rates))
	for _, r := range rates {
		if r.RatePPM < 0 {
			return nil, fmt.Errorf("tax rate %s: %w", r.Code, ErrNegativeRate)
		}
		if _, dup := byCode[r.Code]; dup {
			return nil, fmt.Errorf("tax rate %s: %w", r.Code, ErrDuplicateRate)
		}
		byCode[r.Code] = r.RatePPM
	}
	base := map[string]int64{}
	for _, l := range lines {
		for _, c := range l.TaxCodes {
			if c == TaxCodeNone {
				continue
			}
			if _, ok := byCode[c]; !ok {
				return nil, fmt.Errorf("tax line %s code %s: %w", l.ID, c, ErrUnknownTaxCode)
			}
			sum, err := addCents(base[c], l.NetCents)
			if err != nil {
				return nil, fmt.Errorf("tax base %s: %w", c, err)
			}
			base[c] = sum
		}
	}
	out := make([]TaxLine, 0, len(base))
	for c, b := range base {
		rate := byCode[c]
		num, err := mulCents(b, rate)
		if err != nil {
			return nil, fmt.Errorf("tax %s: %w", c, err)
		}
		tax, err := a.Rhu(num, money.PPMScale)
		if err != nil {
			return nil, fmt.Errorf("tax %s: %w", c, err)
		}
		out = append(out, TaxLine{Code: c, RatePPM: rate, BaseCents: b, TaxCents: tax})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}

func addCents(x, y int64) (int64, error) {
	if (y > 0 && x > math.MaxInt64-y) || (y < 0 && x < math.MinInt64-y) {
		return 0, money.ErrOverflow
	}
	return x + y, nil
}

func mulCents(x, y int64) (int64, error) {
	if x == 0 || y == 0 {
		return 0, nil
	}
	p := x * y
	if p/y != x || (x == -1 && y == math.MinInt64) || (y == -1 && x == math.MinInt64) {
		return 0, money.ErrOverflow
	}
	return p, nil
}
