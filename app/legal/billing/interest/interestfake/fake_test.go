package interestfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/interest"
	"github.com/dagi377/counsel/kernel/money"
)

func toronto(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	return loc
}

func day(loc *time.Location, s string) time.Time {
	d, err := time.ParseInLocation(time.DateOnly, s, loc)
	if err != nil {
		panic(err)
	}
	return d
}

func TestMonthsAfter(t *testing.T) {
	loc := toronto(t)
	tests := []struct{ in, want string }{
		{"2026-01-31", "2026-02-28"},
		{"2026-03-31", "2026-04-30"},
		{"2028-01-30", "2028-02-29"},
		{"2026-01-15", "2026-02-15"},
		{"2026-12-15", "2027-01-15"},
	}
	f := New(loc)
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := f.MonthsAfter(day(loc, tt.in), 1).Format(time.DateOnly); got != tt.want {
				t.Fatalf("MonthsAfter(%s, 1) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestAccrualStart(t *testing.T) {
	loc := toronto(t)
	tests := []struct {
		name      string
		delivered time.Time
		want      string
	}{
		{name: "mid month", delivered: day(loc, "2026-01-15").Add(10 * time.Hour), want: "2026-02-16"},
		{name: "UTC instant is a Toronto date", delivered: time.Date(2026, 11, 1, 3, 30, 0, 0, time.UTC), want: "2026-12-01"},
	}
	f := New(loc)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.AccrualStart(tt.delivered).Format(time.DateOnly); got != tt.want {
				t.Fatalf("AccrualStart = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestSimple(t *testing.T) {
	tests := []struct {
		name      string
		principal money.Cents
		rate      money.PPM
		days      int
		want      money.Cents
		err       error
	}{
		{name: "31 days at 12%", principal: 100000, rate: 120000, days: 31, want: 1019},
		{name: "zero days", principal: 100000, rate: 120000, days: 0, want: 0},
		{name: "overflow", principal: 1 << 40, rate: 1 << 30, days: 1, err: money.ErrOverflow},
	}
	f := New(time.UTC)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Simple(tt.principal, tt.rate, tt.days)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if got != tt.want {
				t.Fatalf("Simple = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPost(t *testing.T) {
	loc := toronto(t)
	delivered := day(loc, "2026-01-15").Add(9 * time.Hour)
	tests := []struct {
		name     string
		rate     money.PPM
		from, to string
		twice    bool
		wantDays int
		wantFrom string
		err      error
	}{
		{name: "no agreed rate, no interest", rate: 0, from: "2026-03-01", to: "2026-04-01", err: interest.ErrNoAgreedRate},
		{name: "period before accrual start", rate: 120000, from: "2026-01-15", to: "2026-02-16", err: interest.ErrNotDue},
		{name: "period clipped to accrual start", rate: 120000, from: "2026-02-01", to: "2026-03-01", wantDays: 13, wantFrom: "2026-02-16"},
		{name: "full period", rate: 120000, from: "2026-03-01", to: "2026-04-01", wantDays: 31, wantFrom: "2026-03-01"},
		{name: "posted twice", rate: 120000, from: "2026-03-01", to: "2026-04-01", twice: true, err: interest.ErrAlreadyPosted},
		{name: "reversed period", rate: 120000, from: "2026-04-01", to: "2026-03-01", err: interest.ErrBadPeriod},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(loc)
			b := interest.Bill{ID: "b1", DeliveredAt: delivered, Principal: 100000, RatePPM: tt.rate}
			p := interest.Period{From: day(loc, tt.from), To: day(loc, tt.to)}
			c, err := f.Post(context.Background(), b, p)
			if tt.twice && err == nil {
				_, err = f.Post(context.Background(), b, p)
			}
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if tt.err != nil {
				return
			}
			if c.Days != tt.wantDays || c.Period.From.Format(time.DateOnly) != tt.wantFrom {
				t.Fatalf("charge days=%d from=%s, want %d %s", c.Days, c.Period.From.Format(time.DateOnly), tt.wantDays, tt.wantFrom)
			}
		})
	}
}
