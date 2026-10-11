package clio

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/money"
)

func toronto(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	return loc
}

// newTestBucket returns a bucket whose sleeps advance the fake clock.
func newTestBucket(t *testing.T, at time.Time, maxOffPeak int) (*Bucket, *clockfake.Fake) {
	t.Helper()
	clk := clockfake.New(at, at.Location())
	sleep := func(_ context.Context, d time.Duration) error {
		if d > 0 {
			clk.Advance(d)
		}
		return nil
	}
	return NewBucket(clk, maxOffPeak, sleep), clk
}

// rule: MIG-003
func TestIsPeak(t *testing.T) {
	loc := toronto(t)
	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"tuesday 10:00", time.Date(2026, 10, 20, 10, 0, 0, 0, loc), true},
		{"tuesday 07:00 start", time.Date(2026, 10, 20, 7, 0, 0, 0, loc), true},
		{"tuesday 06:59", time.Date(2026, 10, 20, 6, 59, 0, 0, loc), false},
		{"tuesday 22:00 end", time.Date(2026, 10, 20, 22, 0, 0, 0, loc), false},
		{"saturday 10:00", time.Date(2026, 10, 24, 10, 0, 0, 0, loc), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPeak(tc.at, loc); got != tc.want {
				t.Errorf("IsPeak = %v, want %v", got, tc.want)
			}
		})
	}
}

// rule: MIG-003
func TestBucketPeakPacing(t *testing.T) {
	loc := toronto(t)
	start := time.Date(2026, 10, 20, 10, 0, 0, 0, loc) // Tuesday 10:00 Toronto
	tests := []struct {
		name    string
		n       int
		minTime time.Duration
		maxTime time.Duration
	}{
		{"100 queued requests take at least 2m13s", 100, 2*time.Minute + 13*time.Second, 2*time.Minute + 14*time.Second},
		{"45 requests take one minute", 45, 59 * time.Second, time.Minute},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, clk := newTestBucket(t, start, 0)
			for i := 0; i < tc.n; i++ {
				if err := b.Wait(context.Background()); err != nil {
					t.Fatalf("Wait: %v", err)
				}
			}
			got := clk.Now().Sub(start)
			if got < tc.minTime || got > tc.maxTime {
				t.Errorf("elapsed %v, want in [%v, %v]", got, tc.minTime, tc.maxTime)
			}
			if r := b.Rate(); r != PeakRate {
				t.Errorf("rate %d, want %d", r, PeakRate)
			}
		})
	}
}

// rule: MIG-003
func TestBucketOffPeakRampAndThrottle(t *testing.T) {
	loc := toronto(t)
	night := time.Date(2026, 10, 20, 23, 0, 0, 0, loc)
	tests := []struct {
		name      string
		quiet     time.Duration
		max       int
		wantRate  int
		retry     time.Duration
		wantAfter int
	}{
		{"starts at 45", 0, 200, 45, 0, 0},
		{"plus 10 per quiet 5 minutes", 20 * time.Minute, 200, 85, 0, 0},
		{"capped at max", 60 * time.Minute, 60, 60, 0, 0},
		{"429 halves 85 to 42", 20 * time.Minute, 200, 85, 30 * time.Second, 42},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, clk := newTestBucket(t, night, tc.max)
			_ = b.Rate()
			clk.Advance(tc.quiet)
			if r := b.Rate(); r != tc.wantRate {
				t.Fatalf("rate %d, want %d", r, tc.wantRate)
			}
			if tc.retry == 0 {
				return
			}
			at := clk.Now()
			b.Throttled(tc.retry)
			if r := b.Rate(); r != tc.wantAfter {
				t.Errorf("rate after 429 %d, want %d", r, tc.wantAfter)
			}
			if err := b.Wait(context.Background()); err != nil {
				t.Fatalf("Wait: %v", err)
			}
			if sent := clk.Now().Sub(at); sent < tc.retry {
				t.Errorf("next request at +%v, want >= +%v", sent, tc.retry)
			}
		})
	}
}

func TestBucketWaitHonoursContext(t *testing.T) {
	loc := toronto(t)
	clk := clockfake.New(time.Date(2026, 10, 20, 10, 0, 0, 0, loc), loc)
	b := NewBucket(clk, 0, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Wait = %v, want context.Canceled", err)
	}
}

// rule: MIG-005
func TestParseCents(t *testing.T) {
	tests := []struct {
		in      string
		want    money.Cents
		wantErr bool
	}{
		{"1234.5", 123450, false},
		{"0.1", 10, false},
		{"1e2", 10000, false},
		{"19.99", 1999, false},
		{"19.990", 1999, false},
		{"-3.25", -325, false},
		{"0", 0, false},
		{"1.5E-1", 15, false},
		{"19.999", 0, true},
		{"0.001", 0, true},
		{"", 0, true},
		{"1,000.00", 0, true},
		{"abc", 0, true},
		{"1e", 0, true},
		{"99999999999999999999", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseCents(tc.in)
			if tc.wantErr {
				if !errors.Is(err, ErrBadDecimal) {
					t.Fatalf("ParseCents(%q) err = %v, want ErrBadDecimal", tc.in, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("ParseCents(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
			}
		})
	}
}

// rule: MIG-005
func TestMoneyCents(t *testing.T) {
	tests := []struct {
		name     string
		amount   any
		currency string
		want     money.Cents
		wantErr  bool
	}{
		{"json number", json.Number("1234.5"), "CAD", 123450, false},
		{"string", "0.10", "cad", 10, false},
		{"USD rejected", json.Number("1"), "USD", 0, true},
		{"non-decimal type rejected", int64(150), "CAD", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MoneyCents(tc.amount, tc.currency)
			if (err != nil) != tc.wantErr || (err != nil && !errors.Is(err, ErrBadDecimal)) {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// rule: MIG-005
func TestDecodeRecordKeepsDecimals(t *testing.T) {
	var rec struct {
		Total    any    `json:"total"`
		Currency string `json:"currency"`
	}
	if err := DecodeRecord(json.RawMessage(`{"total": 0.10, "currency": "CAD"}`), &rec); err != nil {
		t.Fatalf("DecodeRecord: %v", err)
	}
	var sum money.Cents
	for i := 0; i < 10; i++ {
		c, err := MoneyCents(rec.Total, rec.Currency)
		if err != nil {
			t.Fatalf("MoneyCents: %v", err)
		}
		sum += c
	}
	if sum != 100 {
		t.Errorf("10 x 0.10 = %d cents, want 100", sum)
	}
}
