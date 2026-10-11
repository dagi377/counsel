// Package totalfake is an in-memory app/legal/billing/total for tests: no network, no
// Postgres. It computes real totals so callers can rely on its numbers, and lets a test
// force an error.
package totalfake

import (
	"fmt"
	"sort"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/total"
)

var _ total.Calculator = (*Fake)(nil)

// Fake is a total.Calculator. Err, when set, is returned by every call.
type Fake struct {
	Err   error
	Calls int
}

// New returns a fake with no forced error.
func New() *Fake { return &Fake{} }

// rhu rounds n/d half away from zero (BIL-C-002). It stands in for money.Rhu until the
// kernel money package lands; d must be positive.
func rhu(n, d int64) int64 {
	q, r := n/d, n%d
	if r < 0 {
		r = -r
	}
	if 2*r >= d {
		if n < 0 {
			q--
		} else {
			q++
		}
	}
	return q
}

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
		tl := total.TaxLine{Code: c, RatePPM: rates[c], BaseCents: b, TaxCents: rhu(b*rates[c], 1_000_000)}
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
