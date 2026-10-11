//go:build integration

package trust

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/matter/mattertest"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
)

// trustDB creates a database on COUNSEL_TEST_DB, applies the init migration and seeds a user,
// a client, a practice area and two matters. It returns the pool, the user and the matters.
func trustDB(t *testing.T) (*db.Pool, string, string, [2]string) {
	t.Helper()
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "trust_receipt_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.ConnConfig.Database = name
	pool, err := db.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(pool.Close)
	ddl, err := os.ReadFile(filepath.FromSlash("../../../migrations/0001_init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	_, err = conn.PgConn().Exec(ctx, string(ddl)).ReadAll()
	_ = conn.Close(ctx)
	if err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	var user, client string
	var matters [2]string
	err = pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `WITH r AS (INSERT INTO core.role(key, name, status) VALUES ('lawyer', 'Lawyer', 'active') RETURNING id)
INSERT INTO core.app_user(email, display_name, role_id) SELECT 'l@example.com', 'L', id FROM r RETURNING id::text`).Scan(&user); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO party.contact(kind, legal_name, display_name) VALUES ('person', 'Client A', 'Client A') RETURNING id::text`).Scan(&client); err != nil {
			return err
		}
		var area string
		if err := tx.QueryRow(ctx, `INSERT INTO matter.practice_area(name) VALUES ('Litigation') RETURNING id::text`).Scan(&area); err != nil {
			return err
		}
		for i, n := range []string{"2026-0001", "2026-0002"} {
			var err error
			if matters[i], err = mattertest.Seed(ctx, tx, n, n, client, area, user); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return pool, user, client, matters
}

type itUsers struct{}

func (u itUsers) User(_ context.Context, id string) (signatory.User, error) {
	return signatory.User{ID: id, Active: true, Licensee: true}, nil
}

type itSealer struct{}

func (itSealer) Seal(_ context.Context, p []byte) ([]byte, error) {
	return append([]byte("sealed:"), p...), nil
}

// rule: BIL-I-009
func TestBIL_I_009_ReceiptIntegration(t *testing.T) {
	pool, user, client, matters := trustDB(t)
	ctx := context.Background()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 16, 12, 0, 0, 0, loc)
	clk := clockfake.New(now, loc)
	// gl.* holds the operating book only (CF-02), so the trust book is the in-memory ledger.
	gl := ledgerfake.New(clock.DateOf(now, loc), books.AcctTrustBank, books.AcctClientTrustLedger)
	accts := NewAccountStore(pool, itUsers{}, itSealer{})
	s := NewLedgerStore(pool, accts, gl, clk)

	var acct AccountID
	var ledgers [2]LedgerID
	err = pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		acct, err = accts.OpenAccountNumber(ctx, tx, Account{Type: AccountMixed, Institution: "Bank", AccountLast4: "1234",
			Title: "Mixed trust", OpenedOn: now}, "000111234")
		if err != nil {
			return err
		}
		if err := accts.AddApprovedSignatory(ctx, tx, acct, user, "00000000-0000-7000-8000-000000000001", now); err != nil {
			return err
		}
		for i, m := range matters {
			if ledgers[i], err = s.OpenLedger(ctx, tx, Ledger{AccountID: acct, MatterID: m, ClientID: client}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name   string
		ledger int
		cents  int64
		wantA  int64
		wantB  int64
	}{
		{"receipt of 150000 to client A", 0, 150000, 150000, 0},
		{"receipt of 2500 to client B", 1, 2500, 150000, 2500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				_, err := s.Receive(ctx, tx, Receipt{LedgerID: ledgers[tt.ledger], TxnDate: now, Method: MethodEFT,
					Counterparty: "Client A", AmountCents: tt.cents, Purpose: "retainer", ClearedOn: now, PostedBy: user})
				return err
			})
			if err != nil {
				t.Fatalf("receive: %v", err)
			}
			for i, want := range []int64{tt.wantA, tt.wantB} {
				b, err := s.Balance(ctx, ledgers[i])
				if err != nil {
					t.Fatal(err)
				}
				if b.PostedCents != want {
					t.Fatalf("ledger %d balance = %d, want %d", i, b.PostedCents, want)
				}
			}
			var txns int
			if err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM trust.trust_transaction WHERE account_id = $1::uuid`, string(acct)).Scan(&txns); err != nil {
					return err
				}
				return s.CheckJournal(ctx, tx, acct)
			}); err != nil {
				t.Fatalf("journal: %v", err)
			}
			if txns != tt.ledger+1 {
				t.Fatalf("trust_transaction rows = %d, want %d", txns, tt.ledger+1)
			}
			bank, err := gl.Balance(ctx, books.AcctTrustBank, clock.DateOf(now, loc))
			if err != nil {
				t.Fatal(err)
			}
			client, err := gl.Balance(ctx, books.AcctClientTrustLedger, clock.DateOf(now, loc))
			if err != nil {
				t.Fatal(err)
			}
			if int64(bank) != tt.wantA+tt.wantB || int64(client) != -(tt.wantA+tt.wantB) {
				t.Fatalf("TR-1500 = %d, TR-2500 = %d, want %d", bank, client, tt.wantA+tt.wantB)
			}
		})
	}
}
