package total

import (
	"fmt"
	"time"

	"github.com/dagi377/counsel/kernel/money"
)

// Calc is the server-side Calculator.
type Calc struct {
	a money.Arithmetic
}

var _ Calculator = (*Calc)(nil)

// New returns a Calc that rounds through a.
func New(a money.Arithmetic) *Calc { return &Calc{a: a} }

// Compute returns the totals of d.
//
// rule: BIL-I-004
// bill.total = sum(line.net) + sum(tax_line.amount); line nets already carry discount shares.
func (c *Calc) Compute(d Draft) (Totals, error) {
	var t Totals
	for _, l := range d.Lines {
		sum, err := addCents(t.NetCents, l.NetCents)
		if err != nil {
			return Totals{}, fmt.Errorf("total net: %w", err)
		}
		t.NetCents = sum
	}
	tax, err := TaxLines(c.a, d.Lines, d.Rates)
	if err != nil {
		return Totals{}, fmt.Errorf("total tax: %w", err)
	}
	t.Tax = tax
	for _, tl := range tax {
		sum, err := addCents(t.TaxCents, tl.TaxCents)
		if err != nil {
			return Totals{}, fmt.Errorf("total tax: %w", err)
		}
		t.TaxCents = sum
	}
	total, err := addCents(t.NetCents, t.TaxCents)
	if err != nil {
		return Totals{}, fmt.Errorf("total: %w", err)
	}
	t.TotalCents = total
	return t, nil
}

// TaxPoint implements Calculator.
func (c *Calc) TaxPoint(in TaxPointInput) (time.Time, error) { return TaxPoint(in) }

// Recompute returns the server's totals for d. The totals a client sent are never used;
// changed reports whether they differ from the server's, so a caller can record it.
//
// rule: ARC-I-028 (verify)
// Bill totals are computed server-side from lines with rate_ppm/1e6; client-sent totals are
// ignored.
func (c *Calc) Recompute(d Draft, sent Totals) (server Totals, changed bool, err error) {
	server, err = c.Compute(d)
	if err != nil {
		return Totals{}, false, fmt.Errorf("recompute: %w", err)
	}
	changed = sent.NetCents != server.NetCents || sent.TaxCents != server.TaxCents || sent.TotalCents != server.TotalCents
	return server, changed, nil
}
