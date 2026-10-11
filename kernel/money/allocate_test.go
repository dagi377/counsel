package money

import (
	"errors"
	"math"
	"math/big"
	"math/rand"
	"reflect"
	"testing"
)

// TestAllocateAbsorberByOrder shows how an app rule picks the line that absorbs a tied
// remainder: it orders the weights so that line comes first.
func TestAllocateAbsorberByOrder(t *testing.T) {
	tests := []struct {
		name    string
		total   Cents
		weights []int64
		want    []Cents
	}{
		{name: "first line absorbs", total: 1, weights: []int64{1, 1}, want: []Cents{1, 0}},
		{name: "fee line first", total: 101, weights: []int64{5000, 5000}, want: []Cents{51, 50}},
		{name: "zero-weight line never absorbs", total: 1, weights: []int64{0, 1, 1}, want: []Cents{0, 1, 0}},
		{name: "larger remainder beats order", total: 10, weights: []int64{1, 2}, want: []Cents{3, 7}},
		{name: "two cents over three ties", total: 2, weights: []int64{1, 1, 1}, want: []Cents{1, 1, 0}},
		{name: "negative mirrors positive", total: -101, weights: []int64{5000, 5000}, want: []Cents{-51, -50}},
		{name: "zero total", total: 0, weights: []int64{3, 7}, want: []Cents{0, 0}},
		{name: "single line takes all", total: 12345, weights: []int64{9}, want: []Cents{12345}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Allocate(tt.total, tt.weights)
			if err != nil {
				t.Fatalf("Allocate err = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Allocate(%d, %v) = %v, want %v", tt.total, tt.weights, got, tt.want)
			}
		})
	}
}

func TestAllocateErrors(t *testing.T) {
	tests := []struct {
		name    string
		total   Cents
		weights []int64
		err     error
	}{
		{name: "nil weights", total: 1, weights: nil, err: ErrBadWeights},
		{name: "empty weights", total: 1, weights: []int64{}, err: ErrBadWeights},
		{name: "min weight", total: 1, weights: []int64{math.MinInt64, 1}, err: ErrBadWeights},
		{name: "weight sum overflows uint64", total: 1, weights: []int64{math.MaxInt64, math.MaxInt64, 2}, err: ErrOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Allocate(tt.total, tt.weights)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if got != nil {
				t.Fatalf("parts = %v on error, want nil", got)
			}
		})
	}
}

// TestAllocateInvariants checks, against an exact big.Int reference, that the parts sum
// to total, each part is within one cent of its exact share and on the same side of it,
// leftover cents go to the largest remainders with ties to the lowest index, and a
// negative total is the mirror of its positive value.
func TestAllocateInvariants(t *testing.T) {
	tests := []struct {
		name      string
		maxTotal  int64
		maxWeight int64
		maxLines  int
	}{
		{name: "small", maxTotal: 1000, maxWeight: 10, maxLines: 6},
		{name: "invoice sized", maxTotal: 100_000_000, maxWeight: 1_000_000, maxLines: 20},
		{name: "extreme", maxTotal: math.MaxInt64, maxWeight: math.MaxInt64 / 64, maxLines: 32},
	}
	rng := rand.New(rand.NewSource(3002))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 2000; i++ {
				total := Cents(rng.Int63n(tt.maxTotal))
				weights := make([]int64, 1+rng.Intn(tt.maxLines))
				for j := range weights {
					if rng.Intn(5) > 0 {
						weights[j] = rng.Int63n(tt.maxWeight)
					}
				}
				weights[rng.Intn(len(weights))]++ // never all zero
				checkAllocation(t, total, weights)
			}
		})
	}
}

func checkAllocation(t *testing.T, total Cents, weights []int64) {
	t.Helper()
	got, err := Allocate(total, weights)
	if err != nil {
		t.Fatalf("Allocate(%d, %v) err = %v", total, weights, err)
	}
	if len(got) != len(weights) {
		t.Fatalf("len = %d, want %d", len(got), len(weights))
	}
	sum := new(big.Int)
	for _, w := range weights {
		sum.Add(sum, big.NewInt(w))
	}
	tot := big.NewInt(int64(total))
	floors := make([]*big.Int, len(weights))
	rems := make([]*big.Int, len(weights))
	given := new(big.Int)
	for i, w := range weights {
		q, r := new(big.Int).QuoRem(new(big.Int).Mul(tot, big.NewInt(w)), sum, new(big.Int))
		floors[i], rems[i] = q, r
		given.Add(given, q)
	}
	// Reference: hand the leftover cents to the largest remainders, lowest index first.
	want := make([]int64, len(weights))
	for i := range floors {
		want[i] = floors[i].Int64()
	}
	left := new(big.Int).Sub(tot, given).Int64()
	used := make([]bool, len(weights))
	for ; left > 0; left-- {
		best := -1
		for i := range rems {
			if !used[i] && (best == -1 || rems[i].Cmp(rems[best]) > 0) {
				best = i
			}
		}
		want[best]++
		used[best] = true
	}
	var parts big.Int
	for i, p := range got {
		parts.Add(&parts, big.NewInt(int64(p)))
		if int64(p) != want[i] {
			t.Fatalf("Allocate(%d, %v)[%d] = %d, want %d", total, weights, i, p, want[i])
		}
		if d := int64(p) - floors[i].Int64(); d != 0 && (d != 1 || rems[i].Sign() == 0) {
			t.Fatalf("Allocate(%d, %v)[%d] = %d, exact share floor %d rem %v", total, weights, i, p, floors[i], rems[i])
		}
	}
	if parts.Cmp(tot) != 0 {
		t.Fatalf("Allocate(%d, %v) sums to %v", total, weights, &parts)
	}
	neg, err := Allocate(-total, weights)
	if err != nil {
		t.Fatalf("Allocate(%d, %v) err = %v", -total, weights, err)
	}
	for i := range got {
		if neg[i] != -got[i] {
			t.Fatalf("Allocate(%d, %v) = %v, not the mirror of %v", -total, weights, neg, got)
		}
	}
}

// TestAllocateMinInt64 covers the one total whose magnitude does not fit in int64.
func TestAllocateMinInt64(t *testing.T) {
	tests := []struct {
		name    string
		weights []int64
		want    []Cents
	}{
		{name: "one line", weights: []int64{1}, want: []Cents{math.MinInt64}},
		{name: "two equal", weights: []int64{1, 1}, want: []Cents{math.MinInt64 / 2, math.MinInt64 / 2}},
		{name: "three equal", weights: []int64{1, 1, 1}, want: []Cents{-3074457345618258603, -3074457345618258603, -3074457345618258602}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Allocate(math.MinInt64, tt.weights)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Allocate(MinInt64, %v) = %v, want %v", tt.weights, got, tt.want)
			}
		})
	}
}
