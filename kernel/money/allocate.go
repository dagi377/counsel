package money

import "math/bits"

// Allocate splits total across weights by largest remainder. The parts always sum to
// total; ties in remainder go to the lowest index. Weights must be non-negative and not
// all zero.
//
// It is the only split primitive for money. Callers (app rules) choose the weights and,
// through their order, which line absorbs a tied remainder: put that line first. Each
// part differs from its exact share total*w/sum by less than one cent, and a negative
// total splits as the mirror image of its positive value.
func Allocate(total Cents, weights []int64) ([]Cents, error) {
	var sum uint64
	for _, w := range weights {
		if w < 0 {
			return nil, ErrBadWeights
		}
		var c uint64
		sum, c = bits.Add64(sum, uint64(w), 0)
		if c != 0 {
			return nil, ErrOverflow
		}
	}
	if sum == 0 {
		return nil, ErrBadWeights
	}
	abs := uabs(int64(total))
	parts := make([]uint64, len(weights))
	rems := make([]uint64, len(weights))
	var given uint64
	for i, w := range weights {
		hi, lo := bits.Mul64(abs, uint64(w))
		// hi < sum always holds since w <= sum, so Div64 does not panic.
		q, r := bits.Div64(hi, lo, sum)
		parts[i], rems[i] = q, r
		given += q
	}
	left := abs - given // < len(weights)
	for ; left > 0; left-- {
		best := -1
		for i := range rems {
			if best == -1 || rems[i] > rems[best] {
				best = i
			}
		}
		parts[best]++
		rems[best] = 0
	}
	out := make([]Cents, len(parts))
	for i, p := range parts {
		v, err := signed(p, total < 0)
		if err != nil {
			return nil, err
		}
		out[i] = Cents(v)
	}
	return out, nil
}
