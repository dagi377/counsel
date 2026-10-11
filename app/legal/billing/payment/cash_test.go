package payment_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/billing/payment/paymentfake"
	"github.com/dagi377/counsel/app/legal/trust"
)

// The trust ledger store is the one cash limit for both paths.
var _ payment.CashLimit = (*trust.LedgerStore)(nil)

// cashLimit refuses everything at or over limit unless the gate "g6c" is given.
type cashLimit struct {
	limit int64
	calls []string
}

func (c *cashLimit) CheckOperatingCash(_ context.Context, _ pgx.Tx, matterID string, amount int64, code, gateID string) error {
	c.calls = append(c.calls, matterID)
	if amount < c.limit {
		return nil
	}
	if code == "" {
		return trust.ErrCashLimit
	}
	if gateID != "g6c" {
		return trust.ErrCashGateRequired
	}
	return nil
}

// rule: BIL-L-039
func TestBIL_L_039_OperatingCashPaymentChecksLimit(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		matter    string
		amount    int64
		code      string
		gate      string
		noLimit   bool
		wantErr   error
		wantCalls int
	}{
		{"cheque is not counted", "cheque", "M1", 900000, "", "", false, nil, 0},
		{"cash under the limit captured", payment.MethodCash, "M1", 10000, "", "", false, nil, 1},
		{"cash at the limit refused", payment.MethodCash, "M1", 750000, "", "", false, trust.ErrCashLimit, 1},
		{"cash with exception but no gate refused", payment.MethodCash, "M1", 750000, trust.CashFromFinancialInstitution, "", false, trust.ErrCashGateRequired, 1},
		{"cash with exception and approved gate captured", payment.MethodCash, "M1", 750000, trust.CashFromFinancialInstitution, "g6c", false, nil, 1},
		{"cash with no matter refused", payment.MethodCash, "", 100, "", "", false, payment.ErrInvalid, 0},
		{"cash with no limit configured fails closed", payment.MethodCash, "M1", 100, "", "", true, payment.ErrCashUnchecked, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := paymentfake.New()
			lim := &cashLimit{limit: 750000}
			if !tt.noLimit {
				f.Cash = lim
			}
			f.AddBill("B1", "C1", 1000000, 0)
			p, _, err := f.Capture(context.Background(), payment.Capture{ClientID: "C1", BillID: "B1", Kind: payment.KindBillPayment,
				AmountCents: tt.amount, Method: tt.method, MatterID: tt.matter, CashExceptionCode: tt.code, CashGateID: tt.gate})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("capture err = %v, want %v", err, tt.wantErr)
			}
			if (err == nil) != (p.ID != "") {
				t.Fatalf("payment %q recorded with err %v", p.ID, err)
			}
			if len(lim.calls) != tt.wantCalls {
				t.Fatalf("cash limit called %d times, want %d", len(lim.calls), tt.wantCalls)
			}
		})
	}
}
