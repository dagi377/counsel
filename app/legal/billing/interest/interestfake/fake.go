// Package interestfake is an in-memory implementation of the app/legal/billing/interest
// contract for tests.
package interestfake

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/interest"
	"github.com/dagi377/counsel/kernel/money"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

var _ interest.Service = (*Fake)(nil)

type key struct {
	bill string
	from string
}

// Fake keeps posted charges in memory. A Fake is safe for concurrent use.
type Fake struct {
	mu      sync.Mutex
	loc     *time.Location
	m       *moneyfake.Fake
	charges map[key]interest.Charge
}

// New returns a Fake using the business time zone loc.
func New(loc *time.Location) *Fake {
	return &Fake{loc: loc, m: moneyfake.New(), charges: map[key]interest.Charge{}}
}

// MonthsAfter adds n calendar months, clamping to the last day of the target month.
func (f *Fake) MonthsAfter(d time.Time, n int) time.Time {
	y, mo, day := d.Date()
	first := time.Date(y, mo+time.Month(n), 1, 0, 0, 0, 0, d.Location())
	last := first.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, d.Location())
}

// AccrualStart is the day after one calendar month from the local delivery date.
func (f *Fake) AccrualStart(deliveredAt time.Time) time.Time {
	y, m, d := deliveredAt.In(f.loc).Date()
	local := time.Date(y, m, d, 0, 0, 0, 0, f.loc)
	return f.MonthsAfter(local, 1).AddDate(0, 0, 1)
}

// Simple returns rhu(principal*rate*days, 1e6*365).
func (f *Fake) Simple(principal money.Cents, rate money.PPM, days int) (money.Cents, error) {
	num := int64(principal) * int64(rate)
	if rate != 0 && num/int64(rate) != int64(principal) {
		return 0, money.ErrOverflow
	}
	n2 := num * int64(days)
	if days != 0 && n2/int64(days) != num {
		return 0, money.ErrOverflow
	}
	v, err := f.m.Rhu(n2, money.PPMScale*365)
	if err != nil {
		return 0, fmt.Errorf("simple: %w", err)
	}
	return money.Cents(v), nil
}

// Post accrues interest for the part of p on or after the accrual start, once per bill
// per period start.
func (f *Fake) Post(_ context.Context, b interest.Bill, p interest.Period) (interest.Charge, error) {
	if b.RatePPM <= 0 {
		return interest.Charge{}, fmt.Errorf("post %s: %w", b.ID, interest.ErrNoAgreedRate)
	}
	if !p.To.After(p.From) {
		return interest.Charge{}, fmt.Errorf("post %s: %w", b.ID, interest.ErrBadPeriod)
	}
	from := p.From
	if start := f.AccrualStart(b.DeliveredAt); from.Before(start) {
		from = start
	}
	if !p.To.After(from) {
		return interest.Charge{}, fmt.Errorf("post %s: %w", b.ID, interest.ErrNotDue)
	}
	days := daysBetween(from, p.To)
	amt, err := f.Simple(b.Principal, b.RatePPM, days)
	if err != nil {
		return interest.Charge{}, fmt.Errorf("post %s: %w", b.ID, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	k := key{b.ID, from.Format(time.DateOnly)}
	if _, ok := f.charges[k]; ok {
		return interest.Charge{}, fmt.Errorf("post %s: %w", b.ID, interest.ErrAlreadyPosted)
	}
	c := interest.Charge{
		BillID:    b.ID,
		Period:    interest.Period{From: from, To: p.To},
		Principal: b.Principal,
		RatePPM:   b.RatePPM,
		Days:      days,
		Amount:    amt,
	}
	f.charges[k] = c
	return c, nil
}

// daysBetween counts calendar days in [from, to), ignoring clock changes.
func daysBetween(from, to time.Time) int {
	a := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}
