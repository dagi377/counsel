package tax

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/money"
)

// rule: BIL-C-206
func TestBIL_C_206_NoTaxOnTax(t *testing.T) {
	on := day(2026, time.October, 11)
	cases := []struct {
		name    string
		base    money.Cents
		place   Place
		want    []Line
		wantErr error
	}{
		{"QC GST and QST on 200000", 200000, Place{Province: "QC", Components: []Component{GST, QST}}, []Line{
			{Component: GST, Jurisdiction: "CA", RatePPM: 50000, Base: 200000, Amount: 10000},
			{Component: QST, Jurisdiction: "QC", RatePPM: 99750, Base: 200000, Amount: 19950},
		}, nil},
		{"QC fees 100000 (BIL-T-041)", 100000, Place{Province: "QC", Components: []Component{GST, QST}}, []Line{
			{Component: GST, Jurisdiction: "CA", RatePPM: 50000, Base: 100000, Amount: 5000},
			{Component: QST, Jurisdiction: "QC", RatePPM: 99750, Base: 100000, Amount: 9975},
		}, nil},
		{"BC GST and PST, each rounded once", 333, Place{Province: "BC", Components: []Component{GST, PST}}, []Line{
			{Component: GST, Jurisdiction: "CA", RatePPM: 50000, Base: 333, Amount: 17},
			{Component: PST, Jurisdiction: "BC", RatePPM: 70000, Base: 333, Amount: 23},
		}, nil},
		{"ON HST on 1000", 1000, Place{Province: "ON", Components: []Component{HST}}, []Line{
			{Component: HST, Jurisdiction: "ON", RatePPM: 130000, Base: 1000, Amount: 130},
		}, nil},
		{"zero-rated place", 100000, Place{Province: Zero, Components: []Component{HST}}, []Line{}, nil},
		{"None component", 100000, Place{Province: "AB", Components: []Component{None}}, []Line{}, nil},
		{"no rate for component", 100000, Place{Province: "AB", Components: []Component{PST}}, nil, ErrNoRate},
	}
	calc := NewCalculator(NewRateTable(Seed()))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := calc.Compute(context.Background(), tc.base, tc.place, on)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
