// Package totalfake is an in-memory app/legal/billing/total for tests: no network, no
// Postgres. It computes real totals so callers can rely on its numbers, and lets a test
// force an error.
package totalfake

import (
	"fmt"
	"sort"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/total"
	"github.com/dagi377/counsel/kernel/money"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

var _ total.Calculator = (*Fake)(nil)

// Fake is a total.Calculator. Err, when set, is returned by every call.
type Fake struct {
	Err   error
	Calls int
}

// New returns a fake with no forced error.
func New() *Fake { return &Fake{} }

// rounder is the kernel rounding function (BIL-C-002).
var rounder = moneyfake.New()

// Compute implements total.Calculator.
func (f *Fake) Compute(d total.Draft) (total.Totals, error) {
	f.Calls++
	if f.Err != nil {
		return total.Totals{}, f.Err
	}
	rates := map[string]int64{}
	for _, r := range d.Rates {
		if r.RatePPM < 0 {
			return total.Totals{}, fmt.Errorf("compute %s: %w", r.Code, total.ErrNegativeRate)
		}
		if _, ok := rates[r.Code]; ok {
			return total.Totals{}, fmt.Errorf("compute %s: %w", r.Code, total.ErrDuplicateRate)
		}
		rates[r.Code] = r.RatePPM
	}
	var out total.Totals
	base := map[string]int64{}
	for _, l := range d.Lines {
		out.NetCents += l.NetCents
		for _, c := range l.TaxCodes {
			if _, ok := rates[c]; !ok {
				return total.Totals{}, fmt.Errorf("compute line %s code %s: %w", l.ID, c, total.ErrUnknownTaxCode)
			}
			base[c] += l.NetCents
		}
	}
	for c, b := range base {
		tax, err := rounder.Rhu(b*rates[c], money.PPMScale)
		if err != nil {
			return total.Totals{}, fmt.Errorf("compute tax %s: %w", c, err)
		}
		tl := total.TaxLine{Code: c, RatePPM: rates[c], BaseCents: b, TaxCents: tax}
		out.Tax = append(out.Tax, tl)
		out.TaxCents += tl.TaxCents
	}
	sort.Slice(out.Tax, func(i, j int) bool { return out.Tax[i].Code < out.Tax[j].Code })
	out.TotalCents = out.NetCents + out.TaxCents
	return out, nil
}

// TaxPoint implements total.Calculator.
func (f *Fake) TaxPoint(in total.TaxPointInput) (time.Time, error) {
	f.Calls++
	if f.Err != nil {
		return time.Time{}, f.Err
	}
	due := in.ApprovedOn
	if !in.DueOn.IsZero() && (due.IsZero() || in.DueOn.Before(due)) {
		due = in.DueOn
	}
	switch {
	case due.IsZero() && in.PaidOn.IsZero():
		return time.Time{}, total.ErrNoTaxPoint
	case due.IsZero():
		return in.PaidOn, nil
	case in.PaidOn.IsZero() || due.Before(in.PaidOn):
		return due, nil
	default:
		return in.PaidOn, nil
	}
}
