package surcharge

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/flags/flagsfake"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

func calc(t *testing.T, on bool) Calc {
	t.Helper()
	f := flagsfake.New()
	if err := f.Define(flags.Def{Name: Flag, Owner: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(Flag, on); err != nil {
		t.Fatal(err)
	}
	return Calc{Money: moneyfake.New(), Flags: f}
}

func eligible() Snapshot {
	return Snapshot{FirmSettingOn: true, NoticeDatePassed: true, SurchargeFreeOffered: true,
		Funding: FundingCredit, Destination: BookOperating, LSOCheckAttached: true}
}

var onBill = []TaxLine{{Code: "HST-ON", Cents: 13000}}

// rule: BIL-C-100
func TestBIL_C_100(t *testing.T) {
	tests := []struct {
		name   string
		flagOn bool
		edit   func(*Snapshot)
		want   bool
	}{
		{"all hold", true, func(*Snapshot) {}, true},
		{"flag off by default", false, func(*Snapshot) {}, false},
		{"debit card", true, func(s *Snapshot) { s.Funding = FundingDebit }, false},
		{"prepaid card", true, func(s *Snapshot) { s.Funding = FundingPrepaid }, false},
		{"wallet", true, func(s *Snapshot) { s.Funding = FundingWallet }, false},
		{"Quebec billing address", true, func(s *Snapshot) { s.BillingAddressInQuebec = true }, false},
		{"Quebec firm", true, func(s *Snapshot) { s.FirmInQuebec = true }, false},
		{"retainer to trust", true, func(s *Snapshot) { s.Destination = BookTrust }, false},
		{"firm setting off", true, func(s *Snapshot) { s.FirmSettingOn = false }, false},
		{"notice not passed", true, func(s *Snapshot) { s.NoticeDatePassed = false }, false},
		{"no surcharge-free method", true, func(s *Snapshot) { s.SurchargeFreeOffered = false }, false},
		{"no LSO check", true, func(s *Snapshot) { s.LSOCheckAttached = false }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := calc(t, tc.flagOn)
			s := eligible()
			tc.edit(&s)
			if got := c.Eligible(s); got != tc.want {
				t.Fatalf("Eligible = %v, want %v", got, tc.want)
			}
			ch, err := c.Compute(s, 113000, 24000, 30000, 113000, 13000, onBill)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.want && ch.TotalCents != 0 {
				t.Fatalf("ineligible surcharge = %d, want 0", ch.TotalCents)
			}
		})
	}
}

// rule: BIL-C-101
func TestBIL_C_101(t *testing.T) {
	tests := []struct {
		name               string
		configured, cost   int64
		applied, wantCents int64
		wantPPM            int64
	}{
		{"BIL-X-013 at the cap", 24000, 30000, 113000, 2712, 24000},
		{"configured above cap", 30000, 50000, 113000, 2712, 24000},
		{"cost below cap", 24000, 18000, 113000, 2034, 18000},
		{"configured below cost", 10000, 18000, 113000, 1130, 10000},
		{"rounds half away from zero", 24000, 24000, 2021, 49, 24000}, // 48.504
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch, err := calc(t, true).Compute(eligible(), tc.applied, tc.configured, tc.cost, 113000, 13000, onBill)
			if err != nil {
				t.Fatal(err)
			}
			if ch.RatePPM != tc.wantPPM || ch.Cents != tc.wantCents {
				t.Fatalf("got ppm %d cents %d, want %d %d", ch.RatePPM, ch.Cents, tc.wantPPM, tc.wantCents)
			}
		})
	}
}

// rule: BIL-C-208
// rule: BIL-T-044
func TestBIL_C_208(t *testing.T) {
	tests := []struct {
		name               string
		billTotal, billTax int64
		lines              []TaxLine
		want               []TaxLine
		wantTotal          int64
	}{
		{"BIL-T-044 golden: ON HST", 113000, 13000, onBill, []TaxLine{{"HST-ON", 353}}, 3065},
		{"agency disbursement dilutes the rate", 123000, 13000, onBill, []TaxLine{{"HST-ON", 321}}, 3033}, // 2712*13000/110000 = 320.5
		{"two tax lines", 114975, 14975, []TaxLine{{"GST", 5000}, {"QST", 9975}}, []TaxLine{{"GST", 136}, {"QST", 271}}, 3119},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch, err := calc(t, true).Compute(eligible(), 113000, 24000, 24000, tc.billTotal, tc.billTax, tc.lines)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ch.Tax, tc.want) || ch.TotalCents != tc.wantTotal {
				t.Fatalf("tax %v total %d, want %v %d", ch.Tax, ch.TotalCents, tc.want, tc.wantTotal)
			}
		})
	}
}

// rule: BIL-C-102
func TestBIL_C_102(t *testing.T) {
	c := calc(t, true)
	ch, err := c.Compute(eligible(), 113000, 24000, 24000, 113000, 13000, onBill)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		refunded  int64
		wantCents int64
		wantTax   int64
		wantErr   error
	}{
		{"BIL-X-029 half refund", 56500, 1356, 177, nil},
		{"BIL-T-044 full refund returns exactly", 113000, 2712, 353, nil},
		{"over-refund", 113001, 0, 0, ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := c.Refund(ch, tc.refunded, 113000)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if r.Cents != tc.wantCents || r.Tax[0].Cents != tc.wantTax {
				t.Fatalf("refund %d tax %d, want %d %d", r.Cents, r.Tax[0].Cents, tc.wantCents, tc.wantTax)
			}
		})
	}
}

// rule: BIL-I-022
// rule: BIL-I-015
func TestBIL_I_022(t *testing.T) {
	debit := eligible()
	debit.Funding = FundingDebit
	trust := eligible()
	trust.Destination = BookTrust
	tests := []struct {
		name      string
		flagOn    bool
		snap      Snapshot
		surcharge int64
		want      error
	}{
		{"eligible surcharge", true, eligible(), 2712, nil},
		{"debit card payment with surcharge refused", true, debit, 2712, ErrIneligible},
		{"BIL-I-015 surcharge to trust refused", true, trust, 2712, ErrTrust},
		{"flag off refuses any surcharge", false, eligible(), 1, ErrIneligible},
		{"zero surcharge always fine", true, debit, 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := calc(t, tc.flagOn).Check(tc.snap, tc.surcharge); !errors.Is(err, tc.want) {
				t.Fatalf("Check = %v, want %v", err, tc.want)
			}
		})
	}
}
