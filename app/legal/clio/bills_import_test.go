package clio

import (
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/kernel/flags"
)

func flagSet(t *testing.T, on bool) *flags.Set {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagBylaw9Verified, Owner: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(FlagBylaw9Verified, on); err != nil {
		t.Fatal(err)
	}
	return s
}

// rule: MIG-023 (verify)
func TestMIG_023_TaxNotRecomputed(t *testing.T) {
	line := func(tax int64) []SourceBillLine {
		return []SourceBillLine{{TaxCode: "HST", BaseCents: 100000, RatePPM: 130000, TaxCents: tax}}
	}
	tests := []struct {
		name     string
		flag     bool
		bill     SourceBill
		wantErr  error
		wantTax  int64
		wantWarn []string
	}{
		{"matching tax kept", true, SourceBill{SourceID: "b1", SubtotalCents: 100000, Lines: line(13000)}, nil, 13000, nil},
		{"one cent short kept with warning", true, SourceBill{SourceID: "b1", SubtotalCents: 100000, Lines: line(12999)}, nil, 12999,
			[]string{"tax differs from 13% by 1 cent"}},
		{"five cents over kept with warning", true, SourceBill{SourceID: "b1", SubtotalCents: 100000, Lines: line(13005)}, nil, 13005,
			[]string{"tax differs from 13% by 5 cents"}},
		{"fractional rate", true, SourceBill{SourceID: "b1", SubtotalCents: 1000, Lines: []SourceBillLine{{BaseCents: 1000, RatePPM: 45000, TaxCents: 40}}}, nil, 40,
			[]string{"tax differs from 4.5% by 5 cents"}},
		{"flag off", false, SourceBill{SourceID: "b1", SubtotalCents: 100000, Lines: line(13000)}, ErrBillImportDisabled, 0, nil},
		{"no source id", true, SourceBill{SubtotalCents: 100000, Lines: line(13000)}, ErrBadBill, 0, nil},
		{"negative tax", true, SourceBill{SourceID: "b1", SubtotalCents: 100000, Lines: line(-1)}, ErrBadBill, 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ImportBill(flagSet(t, tc.flag), tc.bill)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if got.TaxCents != tc.wantTax || got.Lines[0].TaxCents != tc.wantTax {
				t.Errorf("tax = %d (line %d), want %d", got.TaxCents, got.Lines[0].TaxCents, tc.wantTax)
			}
			if got.TotalCents != got.SubtotalCents+tc.wantTax {
				t.Errorf("total = %d, want %d", got.TotalCents, got.SubtotalCents+tc.wantTax)
			}
			if !slices.Equal(got.Warnings, tc.wantWarn) {
				t.Errorf("warnings = %q, want %q", got.Warnings, tc.wantWarn)
			}
		})
	}
}

// rule: MIG-034
func TestMIG_034_ImportedBillReadOnly(t *testing.T) {
	imported, err := ImportBill(flagSet(t, true), SourceBill{SourceID: "b1", SubtotalCents: 100})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		bill ImportedBill
		want error
	}{
		{"imported bill", imported, ErrHistoryReadOnly},
		{"live bill", ImportedBill{SourceID: "live"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckBillMutation(tc.bill); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
