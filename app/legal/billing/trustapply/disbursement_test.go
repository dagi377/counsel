package trustapply

import (
	"errors"
	"testing"
)

// rule: BIL-C-042 (verify)
func TestBIL_C_042_DisbursementNeedsWithdrawalGateAndDirection(t *testing.T) {
	ok := TrustDisbursement{
		ExpenseID: "e1", MatterID: "m1", AmountCents: 120000,
		GateID: "g1", GateKind: GateTrustWithdrawal, GateApproved: true, GateAmountCents: 120000,
		ClientDirectionRef: "doc-1",
	}
	tests := []struct {
		name   string
		flagOn bool
		mutate func(*TrustDisbursement)
		want   error
	}{
		{name: "approved gate and direction", flagOn: true, mutate: func(*TrustDisbursement) {}},
		{name: "flag off", flagOn: false, mutate: func(*TrustDisbursement) {}, want: ErrDisabled},
		{name: "flag off fails closed", flagOn: false, mutate: func(*TrustDisbursement) {}, want: ErrUnverified},
		{name: "no gate", flagOn: true, mutate: func(d *TrustDisbursement) { d.GateID = "" }, want: ErrNoWithdrawalGate},
		{name: "wrong gate kind", flagOn: true, mutate: func(d *TrustDisbursement) { d.GateKind = "billing.trust_fee_withdrawal" }, want: ErrNoWithdrawalGate},
		{name: "gate not approved", flagOn: true, mutate: func(d *TrustDisbursement) { d.GateApproved = false }, want: ErrNoWithdrawalGate},
		{name: "gate covers less", flagOn: true, mutate: func(d *TrustDisbursement) { d.GateAmountCents = 119999 }, want: ErrNoWithdrawalGate},
		{name: "no client direction", flagOn: true, mutate: func(d *TrustDisbursement) { d.ClientDirectionRef = "" }, want: ErrNoClientDirection},
		{name: "zero amount", flagOn: true, mutate: func(d *TrustDisbursement) { d.AmountCents = 0 }, want: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := ok
			tt.mutate(&d)
			err := CheckDisbursement(applier(t, tt.flagOn, &store{}), d)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

// rule: BIL-C-042 (verify)
func TestBIL_C_042_TrustPaidExcludedFromAmountDue(t *testing.T) {
	tests := []struct {
		name      string
		lines     []BillDisbursement
		wantDue   int64
		wantTrust int64
		wantErr   error
	}{
		{
			name:      "trust-paid filing fees leave amount due unchanged",
			lines:     []BillDisbursement{{ExpenseID: "courier", AmountCents: 4500}, {ExpenseID: "filing", AmountCents: 120000, PaidFromTrust: true}},
			wantDue:   4500,
			wantTrust: 120000,
		},
		{name: "none from trust", lines: []BillDisbursement{{ExpenseID: "a", AmountCents: 100}}, wantDue: 100},
		{name: "negative line", lines: []BillDisbursement{{ExpenseID: "a", AmountCents: -1}}, wantErr: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SplitDisbursements(tt.lines)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got.DueCents != tt.wantDue || got.PaidFromTrustCents != tt.wantTrust {
				t.Errorf("due %d trust %d, want %d %d", got.DueCents, got.PaidFromTrustCents, tt.wantDue, tt.wantTrust)
			}
			if tt.wantTrust > 0 && len(got.PaidFromTrust) == 0 {
				t.Errorf("trust-paid line not listed under Paid from trust")
			}
		})
	}
}
