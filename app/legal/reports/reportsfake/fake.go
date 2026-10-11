// Package reportsfake is an in-memory reports.Reports for tests in other packages.
package reportsfake

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/reports"
)

var _ reports.Reports = (*Fake)(nil)

type taxLine struct {
	day    time.Time
	cents  int64
	credit bool
}

type openBill struct {
	billed time.Time
	cents  int64
}

type wipEntry struct {
	matter string
	day    time.Time
	cents  int64
}

// Fake computes reports from rows added in memory.
type Fake struct {
	mu        sync.Mutex
	tax       []taxLine
	trust     map[string][]reports.TrustLine
	trustLedg map[string]int64
	bills     []openBill
	arLedger  int64
	wip       []wipEntry
}

// New returns an empty Fake.
func New() *Fake {
	return &Fake{trust: map[string][]reports.TrustLine{}, trustLedg: map[string]int64{}}
}

// AddTax adds a bill (credit=false) or credit note (credit=true) tax line dated day.
func (f *Fake) AddTax(day time.Time, cents int64, credit bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tax = append(f.tax, taxLine{day: day, cents: cents, credit: credit})
}

// SetTrust sets a trust account's month-end lines and its TR-2500 balance.
func (f *Fake) SetTrust(account string, ledger int64, lines ...reports.TrustLine) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trust[account] = lines
	f.trustLedg[account] = ledger
}

// AddOpenBill adds an open bill amount billed on day.
func (f *Fake) AddOpenBill(billed time.Time, cents int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bills = append(f.bills, openBill{billed: billed, cents: cents})
}

// SetARLedger sets the OP-1100 balance.
func (f *Fake) SetARLedger(cents int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.arLedger = cents
}

// AddWIP adds approved unbilled time for matter, worked on day, already priced.
func (f *Fake) AddWIP(matter string, day time.Time, cents int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wip = append(f.wip, wipEntry{matter: matter, day: day, cents: cents})
}

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// HST sums bill tax minus credit note tax in p.
func (f *Fake) HST(_ context.Context, p reports.Period) (reports.HST, error) {
	if dayOf(p.To).Before(dayOf(p.From)) {
		return reports.HST{}, reports.ErrInvalidPeriod
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r := reports.HST{Period: p}
	for _, l := range f.tax {
		if !p.Contains(l.day) {
			continue
		}
		if l.credit {
			r.CreditTaxCents += l.cents
			r.CreditNoteCount++
		} else {
			r.BillTaxCents += l.cents
			r.BillCount++
		}
	}
	r.NetTaxCents = r.BillTaxCents - r.CreditTaxCents
	return r, nil
}

// TrustListing returns the account's lines and checks them against TR-2500.
func (f *Fake) TrustListing(_ context.Context, account string, monthEnd time.Time) (reports.TrustListing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lines, ok := f.trust[account]
	if !ok {
		return reports.TrustListing{}, fmt.Errorf("reportsfake: trust account %s: %w", account, reports.ErrNotFound)
	}
	l := reports.TrustListing{TrustAccountID: account, MonthEnd: monthEnd, Lines: append([]reports.TrustLine(nil), lines...), LedgerCents: f.trustLedg[account]}
	for _, x := range lines {
		l.TotalCents += x.BalanceCents
	}
	if l.TotalCents != l.LedgerCents {
		return l, reports.ErrOutOfBalance
	}
	return l, nil
}

// Aging buckets open bills by days since billing and checks them against OP-1100.
func (f *Fake) Aging(_ context.Context, asOf time.Time) (reports.Aging, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := reports.Aging{AsOf: asOf, LedgerCents: f.arLedger}
	for _, b := range f.bills {
		days := int(dayOf(asOf).Sub(dayOf(b.billed)).Hours() / 24)
		if days < 0 {
			continue
		}
		bucket := reports.DaysOver90
		switch {
		case days <= 30:
			bucket = reports.Days0To30
		case days <= 60:
			bucket = reports.Days31To60
		case days <= 90:
			bucket = reports.Days61To90
		}
		a.Buckets[bucket] += b.cents
		a.TotalCents += b.cents
	}
	if a.TotalCents != a.LedgerCents {
		return a, reports.ErrOutOfBalance
	}
	return a, nil
}

// WIP totals unbilled time on or before asOf by matter.
func (f *Fake) WIP(_ context.Context, asOf time.Time) (reports.WIP, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	by := map[string]int64{}
	for _, e := range f.wip {
		if !dayOf(e.day).After(dayOf(asOf)) {
			by[e.matter] += e.cents
		}
	}
	w := reports.WIP{AsOf: asOf}
	for m, c := range by {
		w.Lines = append(w.Lines, reports.WIPLine{MatterID: m, Cents: c})
		w.TotalCents += c
	}
	sort.Slice(w.Lines, func(i, j int) bool { return w.Lines[i].MatterID < w.Lines[j].MatterID })
	return w, nil
}
