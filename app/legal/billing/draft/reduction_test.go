package draft

import (
	"errors"
	"testing"
)

// rule: BIL-C-021
// rule: BIL-C-023
func TestApplyReductionLineOnly(t *testing.T) {
	base := Line{ID: "l1", Kind: LineFee, AmountCents: 10000, SourceTimeEntryID: "te-1", Taxable: true}
	cases := []struct {
		name    string
		line    Line
		r       Reduction
		wantNet int64
		wantErr error
	}{
		{"partial", base, Reduction{Cents: 2500, Reason: ReasonCourtesy}, 7500, nil},
		{"to zero", base, Reduction{Cents: 10000, Reason: ReasonDuplicate}, 0, nil},
		{"below zero", base, Reduction{Cents: 10001, Reason: ReasonScope}, 10000, ErrNegativeLine},
		{"below zero after discount", Line{ID: "l2", Kind: LineFee, AmountCents: 10000, LineDiscountCents: 9000}, Reduction{Cents: 1001, Reason: ReasonTraining}, 1000, ErrNegativeLine},
		{"other with text", base, Reduction{Cents: 100, Reason: ReasonOther, ReasonText: "agreed"}, 9900, nil},
		{"other without text", base, Reduction{Cents: 100, Reason: ReasonOther}, 10000, ErrReasonRequired},
		{"no reason", base, Reduction{Cents: 100}, 10000, ErrReasonRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.line
			got, err := ApplyReduction(in, tc.r)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if LineNet(got) != tc.wantNet {
				t.Fatalf("net = %d, want %d", LineNet(got), tc.wantNet)
			}
			if got.AmountCents != tc.line.AmountCents || got.SourceTimeEntryID != tc.line.SourceTimeEntryID {
				t.Fatalf("source changed: amount %d entry %q", got.AmountCents, got.SourceTimeEntryID)
			}
			if in.Reduction != nil {
				t.Fatal("input line mutated")
			}
			if err != nil && got.Reduction != nil {
				t.Fatal("reduction set on error")
			}
		})
	}
}
