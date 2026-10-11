package tax

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// rule: BIL-T-020
func TestBIL_T_020_MBRSTIsPST(t *testing.T) {
	on := day(2026, time.October, 11)
	cases := []struct {
		name  string
		place Place
		want  []Line
	}{
		{"MB RST is PST labelled RST", Place{Province: "MB", Components: []Component{GST, PST}}, []Line{
			{Component: GST, Jurisdiction: "CA", RatePPM: 50000, Base: 100000, Amount: 5000},
			{Component: PST, Jurisdiction: "MB", RatePPM: 70000, Base: 100000, Amount: 7000, Label: "RST"},
		}},
		{"BC PST keeps no label", Place{Province: "BC", Components: []Component{GST, PST}}, []Line{
			{Component: GST, Jurisdiction: "CA", RatePPM: 50000, Base: 100000, Amount: 5000},
			{Component: PST, Jurisdiction: "BC", RatePPM: 70000, Base: 100000, Amount: 7000},
		}},
		{"ON HST keeps no label", Place{Province: "ON", Components: []Component{HST}}, []Line{
			{Component: HST, Jurisdiction: "ON", RatePPM: 130000, Base: 100000, Amount: 13000},
		}},
	}
	calc := NewCalculator(NewRateTable(Seed()))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := calc.Compute(context.Background(), 100000, tc.place, on)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// rule: BIL-T-020
func TestSupplyAdditiveFieldsZeroValue(t *testing.T) {
	cases := []struct {
		name string
		s    Supply
	}{
		{"earlier callers set only address, type and date", Supply{ClientProvince: "ON", ServiceType: "general", Date: day(2026, time.October, 11)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.s.PropertyProvince != "" || tc.s.CourtProvince != "" || tc.s.RecipientResident ||
				tc.s.RecipientBusiness || tc.s.MetInCanada || tc.s.AgentRole || tc.s.RegisteredProvinces != nil {
				t.Fatalf("additive fields not zero: %+v", tc.s)
			}
		})
	}
}
