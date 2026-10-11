package interest

import (
	"context"
	"errors"
	"fmt"
	"time"
	_ "time/tzdata" // the delivery quarter is a Toronto date on any host

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/kernel/money"
)

// ErrNoPrejudgmentRate means no Courts of Justice Act prejudgment rate is seeded for the
// quarter asked for (fail closed).
var ErrNoPrejudgmentRate = errors.New("interest: no prejudgment rate for quarter")

// quarter names one calendar quarter, Q is 1..4.
type quarter struct {
	Year int
	Q    int
}

// prejudgmentRates holds the Courts of Justice Act s.127 prejudgment interest rates
// published by the Ontario Ministry of the Attorney General (table for causes of action
// arising after October 23, 1989), in ppm per year. Seeded 2026-10-11 from
// ontario.ca/page/prejudgment-and-postjudgment-interest-rates; a new quarter is added here
// when it is published.
//
// rule: BIL-L-010 (verify)
var prejudgmentRates = map[quarter]money.PPM{
	{2020, 1}: 20000, {2020, 2}: 20000, {2020, 3}: 5000, {2020, 4}: 5000,
	{2021, 1}: 5000, {2021, 2}: 5000, {2021, 3}: 5000, {2021, 4}: 5000,
	{2022, 1}: 5000, {2022, 2}: 5000, {2022, 3}: 18000, {2022, 4}: 28000,
	{2023, 1}: 40000, {2023, 2}: 48000, {2023, 3}: 48000, {2023, 4}: 53000,
	{2024, 1}: 53000, {2024, 2}: 53000, {2024, 3}: 53000, {2024, 4}: 48000,
	{2025, 1}: 40000, {2025, 2}: 33000, {2025, 3}: 30000, {2025, 4}: 30000,
	{2026, 1}: 25000, {2026, 2}: 25000, {2026, 3}: 25000, {2026, 4}: 25000,
}

// PrejudgmentRate returns the Courts of Justice Act prejudgment rate for the quarter that
// contains the business date d (already an America/Toronto date). A firm can use it as the
// agreed rate's CJA benchmark (BIL-L-010). It is applied without an agreed rate only while
// FlagCJADefaultRate is on (Q-INTEREST-CJA); otherwise BIL-C-071 holds and no rate means no
// interest.
//
// rule: BIL-L-010 (verify)
func PrejudgmentRate(d time.Time) (money.PPM, error) {
	y, m, _ := d.Date()
	q := quarter{Year: y, Q: (int(m)-1)/3 + 1}
	r, ok := prejudgmentRates[q]
	if !ok {
		return 0, fmt.Errorf("%d Q%d: %w", q.Year, q.Q, ErrNoPrejudgmentRate)
	}
	return r, nil
}

// EffectiveRate returns the rate interest runs at on b: the agreed rate when one is set.
// With no agreed rate it returns ErrNoAgreedRate, unless FlagCJADefaultRate is on; then it is
// the prejudgment rate for the quarter of the Toronto date b was delivered.
//
// rule: BIL-L-010 (verify)
func EffectiveRate(ctx context.Context, b Bill) (money.PPM, error) {
	if b.RatePPM > 0 {
		return b.RatePPM, nil
	}
	if !legalflags.Enabled(ctx, FlagCJADefaultRate) {
		return 0, fmt.Errorf("bill %s: %w", b.ID, ErrNoAgreedRate)
	}
	if b.DeliveredAt.IsZero() {
		return 0, fmt.Errorf("bill %s: %w", b.ID, ErrNotDelivered)
	}
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		return 0, fmt.Errorf("bill %s: business zone: %w", b.ID, err)
	}
	y, m, d := b.DeliveredAt.In(loc).Date()
	r, err := PrejudgmentRate(time.Date(y, m, d, 0, 0, 0, 0, loc))
	if err != nil {
		return 0, fmt.Errorf("bill %s: %w", b.ID, err)
	}
	return r, nil
}
