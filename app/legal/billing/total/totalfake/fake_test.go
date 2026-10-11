package totalfake

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/total"
)

func TestCompute(t *testing.T) {
	hst := []total.Rate{{Code: "HST", RatePPM: 130000}}
	tests := []struct {
		name    string
		d       total.Draft
		want    total.Totals
		wantErr error
	}{
		{
			name: "tax once on summed base, not per line",
			d: total.Draft{Rates: hst, Lines: []total.Line{
				{ID: "a", NetCents: 3333, TaxCodes: []string{"HST"}},
				{ID: "b", NetCents: 3333, TaxCodes: []string{"HST"}},
				{ID: "c", NetCents: 3334, TaxCodes: []string{"HST"}},
			}},
			want: total.Totals{NetCents: 10000, TaxCents: 1300, TotalCents: 11300,
				Tax: []total.TaxLine{{Code: "HST", RatePPM: 130000, BaseCents: 10000, TaxCents: 1300}}},
		},
		{
			name: "half rounds up",
			d:    total.Draft{Rates: hst, Lines: []total.Line{{ID: "a", NetCents: 429890, TaxCodes: []string{"HST"}}}},
			want: total.Totals{NetCents: 429890, TaxCents: 55886, TotalCents: 485776,
				Tax: []total.TaxLine{{Code: "HST", RatePPM: 130000, BaseCents: 429890, TaxCents: 55886}}},
		},
		{
			name: "exempt line has no tax",
			d:    total.Draft{Rates: hst, Lines: []total.Line{{ID: "a", NetCents: 500}}},
			want: total.Totals{NetCents: 500, TotalCents: 500},
		},
		{
			name: "credit line rounds half away from zero",
			d:    total.Draft{Rates: []total.Rate{{Code: "X", RatePPM: 500000}}, Lines: []total.Line{{ID: "a", NetCents: -3, TaxCodes: []string{"X"}}}},
			want: total.Totals{NetCents: -3, TaxCents: -2, TotalCents: -5,
				Tax: []total.TaxLine{{Code: "X", RatePPM: 500000, BaseCents: -3, TaxCents: -2}}},
		},
		{
			name:    "unknown tax code",
			d:       total.Draft{Lines: []total.Line{{ID: "a", NetCents: 1, TaxCodes: []string{"GST"}}}},
			wantErr: total.ErrUnknownTaxCode,
		},
		{
			name:    "duplicate rate",
			d:       total.Draft{Rates: append(hst, hst...)},
			wantErr: total.ErrDuplicateRate,
		},
		{
			name:    "negative rate",
			d:       total.Draft{Rates: []total.Rate{{Code: "X", RatePPM: -1}}},
			wantErr: total.ErrNegativeRate,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New().Compute(tt.d)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Compute = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTaxPoint(t *testing.T) {
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	tests := []struct {
		name    string
		in      total.TaxPointInput
		want    time.Time
		wantErr error
	}{
		{"approved, unpaid", total.TaxPointInput{ApprovedOn: day(10, 31)}, day(10, 31), nil},
		{"paid before approval", total.TaxPointInput{ApprovedOn: day(10, 31), PaidOn: day(10, 2)}, day(10, 2), nil},
		{"paid after approval", total.TaxPointInput{ApprovedOn: day(10, 31), PaidOn: day(11, 5)}, day(10, 31), nil},
		{"due before bill date", total.TaxPointInput{ApprovedOn: day(10, 31), DueOn: day(10, 15)}, day(10, 15), nil},
		{"paid only", total.TaxPointInput{PaidOn: day(9, 1)}, day(9, 1), nil},
		{"nothing", total.TaxPointInput{}, time.Time{}, total.ErrNoTaxPoint},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New().TaxPoint(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("TaxPoint = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestForcedError(t *testing.T) {
	boom := errors.New("boom")
	f := &Fake{Err: boom}
	if _, err := f.Compute(total.Draft{}); !errors.Is(err, boom) {
		t.Fatalf("Compute err = %v", err)
	}
	if _, err := f.TaxPoint(total.TaxPointInput{}); !errors.Is(err, boom) {
		t.Fatalf("TaxPoint err = %v", err)
	}
	if f.Calls != 2 {
		t.Fatalf("Calls = %d, want 2", f.Calls)
	}
}
