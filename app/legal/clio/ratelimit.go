package clio

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

// Rate budget constants (MIG-003), in requests per minute.
const (
	PeakRate     = 45               // fixed during the Clio peak window
	OffPeakStart = 45               // off-peak rate after start or reset
	OffPeakStep  = 10               // off-peak increase per quiet RampEvery
	RampEvery    = 5 * time.Minute  // quiet time needed for one OffPeakStep
	minRate      = 1                // a halved rate never drops below this
	peakStartHr  = 7                // 07:00 America/Toronto (04:00 PT)
	peakEndHr    = 22               // 22:00 America/Toronto (19:00 PT)
	defaultMax   = OffPeakStart * 4 // off-peak cap when none is configured
)

var _ Limiter = (*Bucket)(nil)

// SleepFunc blocks for d or until ctx ends.
type SleepFunc func(ctx context.Context, d time.Duration) error

// Bucket is the client-side token bucket for one source token. Requests are paced one
// interval apart: 45/min in the peak window; off-peak the rate ramps up by 10/min every
// 5 quiet minutes up to Max. A 429 waits Retry-After and halves the off-peak rate.
type Bucket struct {
	mu     sync.Mutex
	clk    clock.Clock
	sleep  SleepFunc
	max    int
	rate   int       // current off-peak rate
	rampAt time.Time // start of the current quiet period
	next   time.Time // earliest time the next request may go
}

// NewBucket returns a bucket on clk. maxOffPeak caps the off-peak rate (0 for the
// default). sleep may be nil for a real timer.
func NewBucket(clk clock.Clock, maxOffPeak int, sleep SleepFunc) *Bucket {
	if maxOffPeak < OffPeakStart {
		maxOffPeak = defaultMax
	}
	if sleep == nil {
		sleep = sleepTimer
	}
	return &Bucket{clk: clk, sleep: sleep, max: maxOffPeak, rate: OffPeakStart}
}

func sleepTimer(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// IsPeak reports whether t is in the Clio peak window: Mon-Fri 07:00-22:00 in the clock's
// business zone (America/Toronto).
//
// rule: MIG-003
func IsPeak(t time.Time, loc *time.Location) bool {
	lt := t.In(loc)
	if wd := lt.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	return lt.Hour() >= peakStartHr && lt.Hour() < peakEndHr
}

// Rate returns the requests per minute the bucket allows at now.
func (b *Bucket) Rate() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rateAt(b.clk.Now())
}

// rateAt applies the off-peak ramp up to now and returns the rate in force. Caller holds mu.
//
// rule: MIG-003
func (b *Bucket) rateAt(now time.Time) int {
	if IsPeak(now, b.clk.Location()) {
		b.rampAt = time.Time{}
		return PeakRate
	}
	if b.rampAt.IsZero() {
		b.rampAt = now
	}
	for now.Sub(b.rampAt) >= RampEvery {
		b.rampAt = b.rampAt.Add(RampEvery)
		b.rate += OffPeakStep
	}
	if b.rate > b.max {
		b.rate = b.max
	}
	return b.rate
}

// Wait reserves the next request slot and blocks until it arrives or ctx ends.
//
// rule: MIG-003
func (b *Bucket) Wait(ctx context.Context) error {
	b.mu.Lock()
	now := b.clk.Now()
	interval := time.Minute / time.Duration(b.rateAt(now))
	slot := b.next
	if slot.Before(now) {
		slot = now
	}
	slot = slot.Add(interval)
	b.next = slot
	b.mu.Unlock()
	if err := b.sleep(ctx, slot.Sub(now)); err != nil {
		return fmt.Errorf("rate limit wait: %w", err)
	}
	return nil
}

// Throttled records a 429: no request goes before now+retryAfter, the off-peak rate halves
// and the quiet period restarts.
//
// rule: MIG-003
func (b *Bucket) Throttled(retryAfter time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clk.Now()
	b.rateAt(now)
	if until := now.Add(retryAfter); until.After(b.next) {
		b.next = until
	}
	b.rate /= 2
	if b.rate < minRate {
		b.rate = minRate
	}
	b.rampAt = now
}
