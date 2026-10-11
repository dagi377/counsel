package trust

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// rule: BIL-I-010
func TestBIL_I_010_LedgerCheckMapsToInsufficientFunds(t *testing.T) {
	commit := func(code, msg string) error {
		// pool.InTx wraps a COMMIT failure; the deferred DM-052 trigger fails there.
		return fmt.Errorf("commit: %w", &pgconn.PgError{Code: code, Message: msg})
	}
	other := errors.New("connection reset")
	tests := []struct {
		name     string
		err      error
		mapped   bool // ErrInsufficientFunds expected
		sameErr  bool // returned unchanged
		noDetail bool // the ledger id and balance in the db message must not leak
	}{
		{"DM-052 at commit", commit("23514", "DM-052: client trust ledger 0190-x would be overdrawn (balance -500 cents)"), true, false, true},
		{"DM-064 at insert", fmt.Errorf("insert txn: %w", &pgconn.PgError{Code: "23514", Message: "DM-064: insufficient available trust funds on ledger 0190-x (available 0, requested 500)"}), true, false, true},
		{"other CHECK violation is not a balance guard", commit("23514", `new row for relation "trust_transaction" violates check constraint "trust_transaction_check"`), false, true, false},
		{"DM-052 text under another code", commit("P0001", "DM-052: client trust ledger x would be overdrawn"), false, true, false},
		{"deadlock is not hidden", commit("40P01", "deadlock detected"), false, true, false},
		{"plain error", other, false, true, false},
		{"no error", nil, false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ledgerCheck(tt.err)
			if errors.Is(got, ErrInsufficientFunds) != tt.mapped {
				t.Fatalf("ledgerCheck = %v, mapped to ErrInsufficientFunds = %v, want %v", got, !tt.mapped, tt.mapped)
			}
			if tt.sameErr && got != tt.err {
				t.Fatalf("ledgerCheck = %v, want the error unchanged (%v)", got, tt.err)
			}
			if tt.noDetail && (strings.Contains(got.Error(), "0190-x") || strings.Contains(got.Error(), "-500")) {
				t.Fatalf("ledgerCheck leaks the database detail: %v", got)
			}
		})
	}
}

// rule: BIL-I-010
func TestBIL_I_010_InLedgerTxMapsCheckViolation(t *testing.T) {
	dm052 := &pgconn.PgError{Code: "23514", Message: "DM-052: client trust ledger x would be overdrawn (balance -1 cents)"}
	boom := errors.New("boom")
	tests := []struct {
		name string
		fn   func(context.Context, pgx.Tx) error
		want error
	}{
		{"commits", func(context.Context, pgx.Tx) error { return nil }, nil},
		{"balance guard -> insufficient funds", func(context.Context, pgx.Tx) error { return fmt.Errorf("post: %w", dm052) }, ErrInsufficientFunds},
		{"other errors pass through", func(context.Context, pgx.Tx) error { return boom }, boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			err := r.s.InLedgerTx(context.Background(), tt.fn)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("InLedgerTx = %v, want %v", err, tt.want)
			}
		})
	}
}
