package payment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
)

func TestFundsOf(t *testing.T) {
	tests := []struct {
		name string
		b    trust.Balance
		held int64
		want withdraw.Funds
	}{
		{"cleared only", trust.Balance{PostedCents: 10_000, ClearedCents: 10_000}, 0, withdraw.Funds{ClearedCents: 10_000}},
		{"uncleared and held", trust.Balance{PostedCents: 15_000, ClearedCents: 10_000, ProtectedCents: 1_000, MinimumCents: 500}, 2_000,
			withdraw.Funds{ClearedCents: 10_000, UnclearedCents: 5_000, ProtectedCents: 1_000, MinimumCents: 500, AuthorizedCents: 2_000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// rule: BIL-L-037
			if got := FundsOf(tt.b, tt.held); got != tt.want {
				t.Fatalf("FundsOf = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRefundStoreRefusesBadInput(t *testing.T) {
	tests := []struct {
		name string
		r    RefundRecord
	}{
		{"zero cents", RefundRecord{PaymentID: "p", ApprovalID: "g", ApprovedBy: "u"}},
		{"negative cents", RefundRecord{PaymentID: "p", AmountCents: -1, ApprovalID: "g", ApprovedBy: "u"}},
		{"no approval", RefundRecord{PaymentID: "p", AmountCents: 100, ApprovedBy: "u"}},
		{"no approver", RefundRecord{PaymentID: "p", AmountCents: 100, ApprovalID: "g"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// rule: BIL-C-064
			if _, err := (PGRefunds{}).RecordRefund(context.Background(), nil, tt.r); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestTrustRefundsRequestRefusesBadInput(t *testing.T) {
	ok := withdraw.Request{AmountCents: 100, PreparedBy: "u", IdempotencyKey: "refund:g"}
	tests := []struct {
		name string
		t    PGTrustRefunds
		r    withdraw.Request
	}{
		{"no gates", PGTrustRefunds{}, ok},
		{"no key", PGTrustRefunds{Gates: nil}, withdraw.Request{AmountCents: 100, PreparedBy: "u"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.t.Request(context.Background(), nil, tt.r); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
	if _, err := (PGTrustRefunds{}).Funds(context.Background(), nil, "a", "m"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Funds without ledgers err = %v", err)
	}
}

func TestRefundSQL(t *testing.T) {
	tests := []struct{ name, sql, want string }{
		{"refund row approved", insertRefundSQL, "'approved'"},
		{"credit used is a refund row", refundCreditSQL, "'refund'"},
		{"payment fully refunded", refundStatusSQL, "'refunded'"},
		{"payment locked", lockRefundableSQL, "FOR UPDATE OF p"},
		// rule: BIL-L-037
		{"trust refund is a client_refund withdrawal", insertRefundWithdrawalSQL, "'client_refund'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.sql, tt.want) {
				t.Fatalf("sql lacks %q", tt.want)
			}
		})
	}
}
