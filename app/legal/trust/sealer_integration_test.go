//go:build integration

package trust

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// The sealed account number survives a round trip through trust_account.account_no_ciphertext,
// and the column never holds the plaintext.
func TestAESSealerAccountNumberIntegration(t *testing.T) {
	pool, _, _, _ := trustDB(t)
	ctx := context.Background()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	seal, err := NewAESSealer(SealKey{ID: 1, Key: bytes.Repeat([]byte{'k'}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	accts := NewAccountStore(pool, itUsers{}, seal)
	tests := []struct {
		name, number, last4 string
	}{
		{name: "nine digits", number: "000111234", last4: "1234"},
		{name: "twelve digits", number: "987654325678", last4: "5678"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stored []byte
			err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				id, err := accts.OpenAccountNumber(ctx, tx, Account{Type: AccountMixed, Institution: "Bank",
					AccountLast4: tt.last4, Title: "Mixed trust " + tt.last4, OpenedOn: time.Date(2026, 3, 16, 0, 0, 0, 0, loc)}, tt.number)
				if err != nil {
					return err
				}
				return tx.QueryRow(ctx, `SELECT account_no_ciphertext FROM trust.trust_account WHERE id = $1::uuid`,
					string(id)).Scan(&stored)
			})
			if err != nil {
				t.Fatalf("open and read back: %v", err)
			}
			if bytes.Contains(stored, []byte(tt.number)) {
				t.Fatal("stored ciphertext holds the account number")
			}
			got, err := seal.Open(ctx, stored)
			if err != nil {
				t.Fatalf("open stored value: %v", err)
			}
			if string(got) != tt.number {
				t.Fatalf("read back %q, want %q", got, tt.number)
			}
		})
	}
}
