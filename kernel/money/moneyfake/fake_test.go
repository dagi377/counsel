package moneyfake

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/dagi377/counsel/kernel/money"
)

func TestRhu(t *testing.T) {
	tests := []struct {
		name     string
		num, den int64
		want     int64
		err      error
	}{
		{name: "tax on 2000.00 at 9.975%", num: 200000 * 99750, den: 1_000_000, want: 19950},
		{name: "half up", num: 5, den: 10, want: 1},
		{name: "half away from zero negative", num: -5, den: 10, want: -1},
		{name: "below half", num: 4, den: 10, want: 0},
		{name: "negative denominator", num: 5, den: -10, want: -1},
		{name: "exact", num: 100, den: 4, want: 25},
		{name: "divide by zero", num: 1, den: 0, err: money.ErrDivideByZero},
		{name: "overflow", num: math.MinInt64, den: -1, err: money.ErrOverflow},
	}
	f := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Rhu(tt.num, tt.den)
			if !errors.Is(err, tt.err) {
				t.Fatalf("Rhu(%d, %d) err = %v, want %v", tt.num, tt.den, err, tt.err)
			}
			if err == nil && got != tt.want {
				t.Fatalf("Rhu(%d, %d) = %d, want %d", tt.num, tt.den, got, tt.want)
			}
		})
	}
}

func TestAllocate(t *testing.T) {
	tests := []struct {
		name    string
		total   money.Cents
		weights []int64
		want    []money.Cents
		err     error
	}{
		{name: "even", total: 300, weights: []int64{1, 1, 1}, want: []money.Cents{100, 100, 100}},
		{name: "remainder to lowest index on tie", total: 100, weights: []int64{1, 1, 1}, want: []money.Cents{34, 33, 33}},
		{name: "largest remainder wins", total: 10, weights: []int64{1, 2, 4}, want: []money.Cents{1, 3, 6}},
		{name: "negative total", total: -100, weights: []int64{1, 1, 1}, want: []money.Cents{-34, -33, -33}},
		{name: "zero weight gets nothing", total: 5, weights: []int64{0, 1}, want: []money.Cents{0, 5}},
		{name: "negative weight", total: 5, weights: []int64{-1, 2}, err: money.ErrBadWeights},
		{name: "all zero weights", total: 5, weights: []int64{0, 0}, err: money.ErrBadWeights},
		{name: "no weights", total: 5, weights: nil, err: money.ErrBadWeights},
	}
	f := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Allocate(tt.total, tt.weights)
			if !errors.Is(err, tt.err) {
				t.Fatalf("Allocate err = %v, want %v", err, tt.err)
			}
			if err != nil {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Allocate(%d, %v) = %v, want %v", tt.total, tt.weights, got, tt.want)
			}
			var sum money.Cents
			for _, p := range got {
				sum += p
			}
			if sum != tt.total {
				t.Fatalf("parts sum to %d, want %d", sum, tt.total)
			}
		})
	}
}
