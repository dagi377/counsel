package total

import (
	"errors"
	"math"
	"math/rand"
	"testing"

	"github.com/dagi377/counsel/kernel/money"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

// rule: BIL-I-004
func TestBIL_I_004_TotalExamples(t *testing.T) {
	tests := []struct {
		name    string
		d       Draft
		net     int64
		tax     int64
		total   int64
		wantErr error
	}{
		{"BIL-X-005", Draft{Lines: []Line{line("f", 120000, hstON), line("d", 26000, hstON)}, Rates: on}, 146000, 18980, 164980, nil},
		{"exempt line", Draft{Lines: []Line{line("f", 10000, hstON), line("a", 16000)}, Rates: on}, 26000, 1300, 27300, nil},
		{"empty", Draft{Rates: on}, 0, 0, 0, nil},
		{"unknown code", Draft{Lines: []Line{line("f", 100, gst)}, Rates: on}, 0, 0, 0, ErrUnknownTaxCode},
		{"net overflow", Draft{Lines: []Line{line("a", math.MaxInt64), line("b", 1)}, Rates: on}, 0, 0, 0, money.ErrOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New(moneyfake.New()).Compute(tt.d)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got.NetCents != tt.net || got.TaxCents != tt.tax || got.TotalCents != tt.total {
				t.Fatalf("got net %d tax %d total %d, want %d %d %d", got.NetCents, got.TaxCents, got.TotalCents, tt.net, tt.tax, tt.total)
			}
		})
	}
}

// rule: BIL-I-004
func TestBIL_I_004_TotalProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	codes := []string{hstON, gst, qst}
	rates := []Rate{{Code: hstON, RatePPM: 130000}, {Code: gst, RatePPM: 50000}, {Code: qst, RatePPM: 99750}}
	c := New(moneyfake.New())
	for i := 0; i < 2000; i++ {
		var d Draft
		d.Rates = rates
		var wantNet int64
		for j, n := 0, rng.Intn(12); j < n; j++ {
			l := Line{ID: "l", NetCents: rng.Int63n(10_000_000)}
			for _, code := range codes {
				if rng.Intn(2) == 0 {
					l.TaxCodes = append(l.TaxCodes, code)
				}
			}
			wantNet += l.NetCents
			d.Lines = append(d.Lines, l)
		}
		got, err := c.Compute(d)
		if err != nil {
			t.Fatalf("draft %d: %v", i, err)
		}
		var taxSum int64
		for _, tl := range got.Tax {
			taxSum += tl.TaxCents
		}
		if got.NetCents != wantNet || got.TaxCents != taxSum || got.TotalCents != wantNet+taxSum {
			t.Fatalf("draft %d: net %d tax %d total %d; want net %d tax %d total %d", i, got.NetCents, got.TaxCents, got.TotalCents, wantNet, taxSum, wantNet+taxSum)
		}
	}
}

// rule: ARC-I-028 (verify)
func TestARC_I_028_ServerRecomputesIgnoringClient(t *testing.T) {
	d := Draft{Lines: []Line{line("f", 123456, hstON)}, Rates: on}
	want := Totals{NetCents: 123456, TaxCents: 16049, TotalCents: 139505}
	tests := []struct {
		name    string
		sent    Totals
		changed bool
	}{
		{"client agrees", want, false},
		{"client total tampered", Totals{NetCents: 123456, TaxCents: 16049, TotalCents: 1}, true},
		{"client tax rounded down", Totals{NetCents: 123456, TaxCents: 16048, TotalCents: 139504}, true},
		{"client sent nothing", Totals{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed, err := New(moneyfake.New()).Recompute(d, tt.sent)
			if err != nil {
				t.Fatal(err)
			}
			if got.NetCents != want.NetCents || got.TaxCents != want.TaxCents || got.TotalCents != want.TotalCents {
				t.Fatalf("got %+v, want %+v", got, want)
			}
			if changed != tt.changed {
				t.Fatalf("changed = %v, want %v", changed, tt.changed)
			}
		})
	}
}
