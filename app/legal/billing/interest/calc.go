package interest

import (
	"fmt"
	"math"

	"github.com/dagi377/counsel/kernel/money"
)

var _ Calculator = Calc{}

// daysInYear is the actual/365 day-count denominator.
const daysInYear int64 = 365

// Calc computes simple interest, actual/365. All division goes through M.Rhu.
type Calc struct {
	M money.Arithmetic
}

// Simple returns rhu(principal*rate*days, 1e6*365). It never wraps: a product that does
// not fit in int64 returns money.ErrOverflow.
func (c Calc) Simple(principal money.Cents, rate money.PPM, days int) (money.Cents, error) {
	if principal < 0 || rate < 0 || days < 0 {
		return 0, fmt.Errorf("simple interest: %w", ErrBadPeriod)
	}
	num, ok := mul(int64(principal), int64(rate))
	if !ok {
		return 0, fmt.Errorf("simple interest: %w", money.ErrOverflow)
	}
	num, ok = mul(num, int64(days))
	if !ok {
		return 0, fmt.Errorf("simple interest: %w", money.ErrOverflow)
	}
	v, err := c.M.Rhu(num, money.PPMScale*daysInYear)
	if err != nil {
		return 0, fmt.Errorf("simple interest: %w", err)
	}
	return money.Cents(v), nil
}

// mul multiplies two non-negative int64 values, reporting false on overflow.
func mul(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if a > math.MaxInt64/b {
		return 0, false
	}
	return a * b, true
}
