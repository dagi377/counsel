// Package taxfake is an in-memory implementation of the app/legal/billing/tax contract for tests.
package taxfake

import (
	"context"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/tax"
	"github.com/dagi377/counsel/kernel/money"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

var _ tax.Service = (*Fake)(nil)

// Fake holds a rate table in memory. Place of supply is the client's province, and the
// components are the ones that have a rate for that province (plus GST where no HST
// applies). It is a stand-in, not the decision table.
type Fake struct {
	Rates []tax.Rate
	m     *moneyfake.Fake
}

// New returns a Fake with the given rate rows.
func New(rates []tax.Rate) *Fake {
	return &Fake{Rates: rates, m: moneyfake.New()}
}

// Lookup returns the rate in effect on the date.
func (f *Fake) Lookup(_ context.Context, province string, c tax.Component, on time.Time) (money.PPM, error) {
	for _, r := range f.Rates {
		if r.Component != c || (c != tax.GST && r.Province != province) {
			continue
		}
		if on.Before(r.EffectiveFrom) || (!r.EffectiveTo.IsZero() && !on.Before(r.EffectiveTo)) {
			continue
		}
		return r.RatePPM, nil
	}
	return 0, fmt.Errorf("lookup %s %s %s: %w", province, c, on.Format(time.DateOnly), tax.ErrNoRate)
}

// Decide places the supply in the client's province.
func (f *Fake) Decide(ctx context.Context, s tax.Supply) (tax.Place, error) {
	if !known[s.ClientProvince] {
		return tax.Place{}, fmt.Errorf("decide %q: %w", s.ClientProvince, tax.ErrUnknownProvince)
	}
	p := tax.Place{Province: s.ClientProvince}
	if _, err := f.Lookup(ctx, s.ClientProvince, tax.HST, s.Date); err == nil {
		p.Components = []tax.Component{tax.HST}
		return p, nil
	}
	p.Components = []tax.Component{tax.GST}
	for _, c := range []tax.Component{tax.QST, tax.PST} {
		if _, err := f.Lookup(ctx, s.ClientProvince, c, s.Date); err == nil {
			p.Components = append(p.Components, c)
		}
	}
	return p, nil
}

// Compute returns one line per component, each on the pre-tax base.
func (f *Fake) Compute(ctx context.Context, base money.Cents, p tax.Place, on time.Time) ([]tax.Line, error) {
	lines := make([]tax.Line, 0, len(p.Components))
	for _, c := range p.Components {
		rate, err := f.Lookup(ctx, p.Province, c, on)
		if err != nil {
			return nil, fmt.Errorf("compute: %w", err)
		}
		amt, err := f.m.Rhu(int64(base)*int64(rate), money.PPMScale)
		if err != nil {
			return nil, fmt.Errorf("compute %s: %w", c, err)
		}
		j := p.Province
		if c == tax.GST {
			j = "CA"
		}
		lines = append(lines, tax.Line{Component: c, Jurisdiction: j, RatePPM: rate, Base: base, Amount: money.Cents(amt)})
	}
	return lines, nil
}

// CheckBN checks the 9-digit, 2-letter, 4-digit format and the Luhn check digit of the
// first nine digits.
func (f *Fake) CheckBN(bn string) error {
	if len(bn) != 15 {
		return tax.ErrBadBN
	}
	for i, ch := range bn {
		digit := ch >= '0' && ch <= '9'
		letter := ch >= 'A' && ch <= 'Z'
		if (i >= 9 && i < 11 && !letter) || ((i < 9 || i >= 11) && !digit) {
			return tax.ErrBadBN
		}
	}
	sum := 0
	for i := 0; i < 9; i++ {
		d := int(bn[i] - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	if sum%10 != 0 {
		return tax.ErrBadBN
	}
	return nil
}

var known = map[string]bool{
	"AB": true, "BC": true, "MB": true, "NB": true, "NL": true, "NS": true, "NT": true,
	"NU": true, "ON": true, "PE": true, "QC": true, "SK": true, "YT": true,
}
