package surcharge

import (
	"errors"
	"reflect"
	"testing"
)

// rule: BIL-T-044 (verify)
func TestSurchargeTaxOnHSTBill(t *testing.T) {
	c, ctx := calc(t, true)
	tests := []struct {
		name     string
		flagOn   bool
		snap     Snapshot
		wantTax  []TaxLine
		wantTot  int64
		wantCent int64
	}{
		{"BIL-X-013 surcharge 2712 carries HST 353", true, eligible(), []TaxLine{{"HST-ON", 353}}, 3065, 2712},
		{"flag off: no surcharge, no tax", false, eligible(), nil, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cc, cctx := c, ctx
			if !tc.flagOn {
				cc, cctx = calc(t, false)
			}
			ch, err := cc.Compute(cctx, tc.snap, 113000, 24000, 24000, 113000, 13000, onBill)
			if err != nil {
				t.Fatal(err)
			}
			if ch.Cents != tc.wantCent || ch.TotalCents != tc.wantTot || !reflect.DeepEqual(ch.Tax, tc.wantTax) {
				t.Fatalf("charge = %+v, want cents %d tax %v total %d", ch, tc.wantCent, tc.wantTax, tc.wantTot)
			}
		})
	}
}

// rule: BIL-C-102
// rule: BIL-T-044 (verify)
func TestRefundNext(t *testing.T) {
	c, ctx := calc(t, true)
	ch, err := c.Compute(ctx, eligible(), 113000, 24000, 24000, 113000, 13000, onBill)
	if err != nil {
		t.Fatal(err)
	}
	half := Charge{RatePPM: 24000, Cents: 1356, Tax: []TaxLine{{"HST-ON", 177}}, TotalCents: 1533}
	tests := []struct {
		name          string
		prior         Charge
		priorRefunded int64
		refunded      int64
		want          Charge
		wantErr       error
	}{
		{"BIL-X-029 first half refunds pro rata", Charge{}, 0, 56500, half, nil},
		{"second half returns exactly the rest", half, 56500, 56500,
			Charge{RatePPM: 24000, Cents: 1356, Tax: []TaxLine{{"HST-ON", 176}}, TotalCents: 1532}, nil},
		{"full refund at once returns the charge", Charge{}, 0, 113000, ch, nil},
		{"partial refund capped at what is left", Charge{RatePPM: 24000, Cents: 2711, Tax: []TaxLine{{"HST-ON", 353}}, TotalCents: 3064}, 100000, 1000,
			Charge{RatePPM: 24000, Cents: 1, Tax: []TaxLine{{"HST-ON", 0}}, TotalCents: 1}, nil},
		{"over-refund refused", half, 56500, 56501, Charge{}, ErrInvalid},
		{"negative refund refused", Charge{}, 0, -1, Charge{}, ErrInvalid},
		{"prior larger than charge refused", Charge{Cents: 2713}, 0, 1, Charge{}, ErrInvalid},
		{"prior with unknown tax code refused", Charge{Cents: 1, Tax: []TaxLine{{"GST", 1}}}, 0, 1, Charge{}, ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.RefundNext(ch, tc.prior, tc.priorRefunded, tc.refunded, 113000)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("refund = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// rule: BIL-C-102
func TestRefundNextSumsToCharge(t *testing.T) {
	c, ctx := calc(t, true)
	ch, err := c.Compute(ctx, eligible(), 113000, 24000, 24000, 113000, 13000, onBill)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		parts []int64
	}{
		{"thirds", []int64{37667, 37667, 37666}},
		{"many small", []int64{1, 1, 112997, 1}},
		{"one", []int64{113000}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var prior Charge
			var done int64
			for _, p := range tc.parts {
				r, err := c.RefundNext(ch, prior, done, p, 113000)
				if err != nil {
					t.Fatal(err)
				}
				prior.Cents += r.Cents
				prior.TotalCents += r.TotalCents
				if prior.Tax == nil {
					prior.Tax = []TaxLine{{"HST-ON", 0}}
				}
				prior.Tax[0].Cents += r.Tax[0].Cents
				done += p
			}
			if prior.Cents != ch.Cents || prior.Tax[0].Cents != ch.Tax[0].Cents || prior.TotalCents != ch.TotalCents {
				t.Fatalf("refunds sum to %+v, want %+v", prior, ch)
			}
		})
	}
}
