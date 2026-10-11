package draft

import (
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/billing/bill"
)

func TestBIL_C_021_PGDraftsTotals(t *testing.T) {
	d := PGDrafts{TaxCode: "HST-ON", Component: "HST", TaxRatePPM: 130000}
	lines := []pgLine{
		{source: bill.SourceTime, amount: 100000, taxCode: "HST-ON"},
		{source: bill.SourceExpense, amount: 20000, taxCode: "HST-ON"},
		{source: bill.SourceExpense, amount: 7000, taxCode: bill.TaxCodeExempt},
	}
	tests := []struct {
		name               string
		discount           int64
		want               error
		tax, total, nontax int64
	}{
		{"no discount", 0, nil, 15600, 142600, 7000},
		{"discount lowers the tax base", 10000, nil, 14300, 131300, 7000},
		{"discount above fees and own costs", 120001, bill.ErrInvalid, 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := d.compute(lines, tt.discount)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if err != nil {
				return
			}
			if got.tax != tt.tax || got.total != tt.total || got.nontaxable != tt.nontax ||
				got.total != got.fees+got.taxable+got.nontaxable-got.discount+got.tax {
				t.Fatalf("totals = %+v", got)
			}
		})
	}
}

func TestBIL_C_023_LineActions(t *testing.T) {
	tests := []struct {
		name   string
		a      bill.LineAction
		amount int64
		keep   bool
		want   error
	}{
		{"bill", bill.LineAction{Action: "bill"}, 5000, true, nil},
		{"hold stays unbilled", bill.LineAction{Action: "hold"}, 0, false, nil},
		{"no charge bills zero", bill.LineAction{Action: "no_charge"}, 0, true, nil},
		{"write-down", bill.LineAction{Action: "write_down", WriteDownCents: 1000, ReasonCode: "courtesy"}, 4000, true, nil},
		{"write-down needs a reason", bill.LineAction{Action: "write_down", WriteDownCents: 1000}, 0, false, ErrReasonRequired},
		{"write-down below zero", bill.LineAction{Action: "write_down", WriteDownCents: 6000, ReasonCode: "courtesy"}, 0, false, ErrNegativeLine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount, keep, err := applyAction(5000, tt.a)
			if !errors.Is(err, tt.want) || amount != tt.amount || keep != tt.keep {
				t.Fatalf("applyAction = %d %v %v, want %d %v %v", amount, keep, err, tt.amount, tt.keep, tt.want)
			}
		})
	}
}
