package taxfake

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/tax"
	"github.com/dagi377/counsel/kernel/money"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func seed() *Fake {
	return New([]tax.Rate{
		{TaxCode: "HST-ON", Component: tax.HST, Province: "ON", RatePPM: 130000, EffectiveFrom: day("2016-07-01")},
		{TaxCode: "HST-NS", Component: tax.HST, Province: "NS", RatePPM: 150000, EffectiveFrom: day("2010-07-01"), EffectiveTo: day("2025-04-01")},
		{TaxCode: "HST-NS", Component: tax.HST, Province: "NS", RatePPM: 140000, EffectiveFrom: day("2025-04-01")},
		{TaxCode: "GST", Component: tax.GST, RatePPM: 50000, EffectiveFrom: day("2008-01-01")},
		{TaxCode: "QST", Component: tax.QST, Province: "QC", RatePPM: 99750, EffectiveFrom: day("2013-01-01")},
	})
}

func TestLookup(t *testing.T) {
	tests := []struct {
		name     string
		province string
		c        tax.Component
		on       string
		want     money.PPM
		err      error
	}{
		{name: "ON HST", province: "ON", c: tax.HST, on: "2026-01-15", want: 130000},
		{name: "NS day before change", province: "NS", c: tax.HST, on: "2025-03-31", want: 150000},
		{name: "NS change day", province: "NS", c: tax.HST, on: "2025-04-01", want: 140000},
		{name: "GST any province", province: "AB", c: tax.GST, on: "2026-01-15", want: 50000},
		{name: "no rate", province: "AB", c: tax.HST, on: "2026-01-15", err: tax.ErrNoRate},
		{name: "before first rate", province: "ON", c: tax.HST, on: "2016-06-30", err: tax.ErrNoRate},
	}
	f := seed()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Lookup(context.Background(), tt.province, tt.c, day(tt.on))
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if got != tt.want {
				t.Fatalf("rate = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDecideAndCompute(t *testing.T) {
	tests := []struct {
		name     string
		province string
		base     money.Cents
		want     []tax.Line
		err      error
	}{
		{name: "ON HST", province: "ON", base: 100000, want: []tax.Line{
			{Component: tax.HST, Jurisdiction: "ON", RatePPM: 130000, Base: 100000, Amount: 13000},
		}},
		{name: "QC GST and QST, no tax on tax", province: "QC", base: 200000, want: []tax.Line{
			{Component: tax.GST, Jurisdiction: "CA", RatePPM: 50000, Base: 200000, Amount: 10000},
			{Component: tax.QST, Jurisdiction: "QC", RatePPM: 99750, Base: 200000, Amount: 19950},
		}},
		{name: "AB GST only", province: "AB", base: 333, want: []tax.Line{
			{Component: tax.GST, Jurisdiction: "CA", RatePPM: 50000, Base: 333, Amount: 17},
		}},
		{name: "unknown province", province: "ZZ", err: tax.ErrUnknownProvince},
	}
	f := seed()
	ctx := context.Background()
	on := day("2026-01-15")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := f.Decide(ctx, tax.Supply{ClientProvince: tt.province, ServiceType: "general", Date: on})
			if !errors.Is(err, tt.err) {
				t.Fatalf("Decide err = %v, want %v", err, tt.err)
			}
			if err != nil {
				return
			}
			got, err := f.Compute(ctx, tt.base, p, on)
			if err != nil {
				t.Fatalf("Compute: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("lines = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCheckBN(t *testing.T) {
	tests := []struct {
		name string
		bn   string
		err  error
	}{
		{name: "valid", bn: "123456782RT0001"},
		{name: "bad check digit", bn: "123456789RT0001", err: tax.ErrBadBN},
		{name: "short", bn: "12345678RT0001", err: tax.ErrBadBN},
		{name: "lowercase program", bn: "123456782rt0001", err: tax.ErrBadBN},
		{name: "letter in account", bn: "123456782RT00A1", err: tax.ErrBadBN},
	}
	f := seed()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := f.CheckBN(tt.bn); !errors.Is(err, tt.err) {
				t.Fatalf("CheckBN(%q) = %v, want %v", tt.bn, err, tt.err)
			}
		})
	}
}
