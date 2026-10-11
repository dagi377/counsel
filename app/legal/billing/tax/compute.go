package tax

import (
	"context"
	"fmt"
	"time"

	"github.com/dagi377/counsel/kernel/money"
)

// Calc computes tax lines from a rate source.
type Calc struct {
	rates Rates
}

var _ Calculator = (*Calc)(nil)

// NewCalculator returns a Calculator that looks rates up in r.
func NewCalculator(r Rates) *Calc { return &Calc{rates: r} }

// Compute returns one line per component at the place of supply. Every line is computed on
// the same pre-tax base and rounded once on its own: a provincial line never includes the
// GST line in its base. A ZERO place or a None component yields no line.
func (c *Calc) Compute(ctx context.Context, base money.Cents, p Place, on time.Time) ([]Line, error) {
	lines := make([]Line, 0, len(p.Components))
	if p.Province == Zero {
		return lines, nil
	}
	for _, comp := range p.Components {
		if comp == None {
			continue
		}
		rate, err := c.rates.Lookup(ctx, p.Province, comp, on)
		if err != nil {
			return nil, fmt.Errorf("compute %s: %w", comp, err)
		}
		// rule: BIL-C-206
		amount, err := money.ApplyPPM(base, rate)
		if err != nil {
			return nil, fmt.Errorf("compute %s: %w", comp, err)
		}
		j := p.Province
		if comp == GST {
			j = "CA"
		}
		lines = append(lines, Line{Component: comp, Jurisdiction: j, RatePPM: rate, Base: base, Amount: amount})
	}
	return lines, nil
}
