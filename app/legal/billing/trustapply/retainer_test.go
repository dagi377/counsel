package trustapply

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
)

func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// rule: BIL-T-051
func TestBIL_T_051_RetainerReceiptPostsNoTax(t *testing.T) {
	tests := []struct {
		name   string
		flag   bool
		amount int64
		err    error
	}{
		{"retainer 300000 received: nothing taxed", true, 300000, nil},
		{"non-positive amount", true, 0, ErrInvalid},
		{"flag off", false, 300000, ErrDisabled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Retainer{}.OnReceipt(legalflagstest.Context(t, FlagBylaw9Verified, tc.flag), tc.amount)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if len(got) != 0 {
				t.Fatalf("tax points = %+v, want none", got)
			}
		})
	}
}

// rule: BIL-T-051
func TestBIL_T_051_TaxArisesOnApplication(t *testing.T) {
	bill := IssuedBill{ID: "b1", IssuedOn: date(2026, 9, 15), TaxCents: 13000}
	tests := []struct {
		name    string
		flag    bool
		bill    IssuedBill
		applied time.Time
		amount  int64
		want    TaxPoint
		err     error
	}{
		{"bill 113000 applied from retainer: HST 13000 via the bill in Q3", true, bill, date(2026, 9, 20), 113000,
			TaxPoint{BillID: "b1", On: date(2026, 9, 15), TaxCents: 13000}, nil},
		{"partial application keeps the bill's tax", true, bill, date(2026, 10, 2), 50000,
			TaxPoint{BillID: "b1", On: date(2026, 9, 15), TaxCents: 13000}, nil},
		{"unissued bill refused", true, IssuedBill{ID: "b1", TaxCents: 13000}, date(2026, 9, 20), 113000, TaxPoint{}, ErrNotDelivered},
		{"no bill refused", true, IssuedBill{}, date(2026, 9, 20), 113000, TaxPoint{}, ErrInvalid},
		{"flag off", false, bill, date(2026, 9, 20), 113000, TaxPoint{}, ErrDisabled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Retainer{}.OnApply(legalflagstest.Context(t, FlagBylaw9Verified, tc.flag), tc.bill, tc.applied, tc.amount)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("tax point = %+v, want %+v", got, tc.want)
			}
		})
	}
}
