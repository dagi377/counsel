// Package moneyfake is an in-memory implementation of the kernel/money contract for tests.
package moneyfake

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/dagi377/counsel/kernel/money"
)

var _ money.Arithmetic = (*Fake)(nil)

// Fake implements money.Arithmetic with math/big, so it never overflows internally and
// reports money.ErrOverflow when a result does not fit in int64.
type Fake struct{}

// New returns a Fake.
func New() *Fake { return &Fake{} }

// Rhu returns num/den rounded half away from zero.
func (f *Fake) Rhu(num, den int64) (int64, error) {
	if den == 0 {
		return 0, money.ErrDivideByZero
	}
	q, err := rhuBig(big.NewInt(num), big.NewInt(den))
	if err != nil {
		return 0, fmt.Errorf("rhu: %w", err)
	}
	return q, nil
}

// Allocate splits total across weights by largest remainder; ties go to the lowest index.
func (f *Fake) Allocate(total money.Cents, weights []int64) ([]money.Cents, error) {
	sum := new(big.Int)
	for _, w := range weights {
		if w < 0 {
			return nil, money.ErrBadWeights
		}
		sum.Add(sum, big.NewInt(w))
	}
	if sum.Sign() == 0 {
		return nil, money.ErrBadWeights
	}
	neg := total < 0
	abs := new(big.Int).Abs(big.NewInt(int64(total)))

	parts := make([]money.Cents, len(weights))
	rems := make([]*big.Int, len(weights))
	given := new(big.Int)
	for i, w := range weights {
		q, r := new(big.Int).QuoRem(new(big.Int).Mul(abs, big.NewInt(w)), sum, new(big.Int))
		parts[i] = money.Cents(q.Int64())
		rems[i] = r
		given.Add(given, q)
	}
	order := make([]int, len(weights))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return rems[order[a]].Cmp(rems[order[b]]) > 0 })
	left := new(big.Int).Sub(abs, given).Int64()
	for k := int64(0); k < left; k++ {
		parts[order[k]]++
	}
	if neg {
		for i := range parts {
			parts[i] = -parts[i]
		}
	}
	return parts, nil
}

// rhuBig rounds num/den half away from zero and checks the result fits in int64.
func rhuBig(num, den *big.Int) (int64, error) {
	if den.Sign() < 0 {
		num = new(big.Int).Neg(num)
		den = new(big.Int).Neg(den)
	}
	q, r := new(big.Int).QuoRem(num, den, new(big.Int))
	// Half away from zero: bump when 2|r| >= den.
	if new(big.Int).Mul(new(big.Int).Abs(r), big.NewInt(2)).Cmp(den) >= 0 {
		if num.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	if !q.IsInt64() {
		return 0, money.ErrOverflow
	}
	return q.Int64(), nil
}
