package draft

import "github.com/dagi377/counsel/kernel/money"

// Calculator is the production Calc: the package's pure draft arithmetic over A. The zero
// value uses money.Default. It never mutates its input slices.
type Calculator struct {
	A money.Arithmetic
}

var _ Calc = Calculator{}

func (c Calculator) arith() money.Arithmetic {
	if c.A == nil {
		return money.Default
	}
	return c.A
}

// Net implements Calc.
func (Calculator) Net(l Line) int64 { return LineNet(l) }

// ApplyReduction implements Calc.
//
// rule: BIL-C-023
func (Calculator) ApplyReduction(l Line, r Reduction) (Line, error) { return ApplyReduction(l, r) }

// ApplyDiscount implements Calc.
//
// rule: BIL-C-022
func (c Calculator) ApplyDiscount(lines []Line, d Discount) ([]Line, error) {
	return ApplyDiscount(c.arith(), lines, d)
}

// Markup implements Calc.
//
// rule: BIL-C-041
func (c Calculator) Markup(kind LineKind, costCents, ppm int64) (int64, error) {
	return Markup(c.arith(), kind, costCents, ppm)
}

// CheckDelivery implements Calc.
//
// rule: BIL-L-002
func (Calculator) CheckDelivery(lines []Line) error { return CheckDelivery(lines) }
