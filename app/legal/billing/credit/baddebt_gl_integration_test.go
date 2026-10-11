//go:build integration

package credit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/migrations"
	"github.com/dagi377/counsel/migrations/migrationstest"
)

// badDebtPool creates a fresh database on the COUNSEL_TEST_DB server with every migration
// applied and returns a pool on it. The database is dropped when the test ends.
func badDebtPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "baddebt_" + hex.EncodeToString(suffix)
	defer migrationstest.LockClusterRoles(ctx, t, admin)()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse COUNSEL_TEST_DB: %v", err)
	}
	cfg.ConnConfig.Database = name
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	defer conn.Close(ctx)
	if err := migrations.Apply(ctx, conn, ""); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestGLBadDebtOnPostgres(t *testing.T) {
	pool := badDebtPool(t)
	ctx := context.Background()
	var user, writeOffID string
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ('staff', 'Staff', 'active') RETURNING id::text`).Scan(&role); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO core.app_user(email, display_name, role_id) VALUES ('lawyer@example.com', 'lawyer', $1::uuid) RETURNING id::text`,
			role).Scan(&user); err != nil {
			return err
		}
		for _, a := range []struct{ code, typ string }{
			{books.AcctBadDebt, "expense"}, {books.AcctSalesTaxPayable, "liability"}, {books.AcctARFees, "asset"},
		} {
			if _, err := tx.Exec(ctx, `INSERT INTO gl.account(code, name, type) VALUES ($1, $1, $2) ON CONFLICT (code) DO NOTHING`, a.code, a.typ); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `SELECT core.uuidv7()::text`).Scan(&writeOffID)
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	g := GLBadDebt{
		Ledger:    &ledger.PG{PostedBy: func(context.Context) (string, error) { return user, nil }},
		WriteOffs: writeOffMap{writeOffID: {ID: writeOffID, BillID: "b1", AmountCents: 59000, Status: StatusApproved}},
	}
	adj := BadDebtAdjustment{BillID: "b1", TaxCents: 6500, On: time.Date(2026, 11, 15, 0, 0, 0, 0, loc)}
	for _, tc := range []struct {
		name string
	}{{"first post"}, {"repeat is a no-op"}} {
		t.Run(tc.name, func(t *testing.T) {
			if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				return g.PostBadDebt(ctx, tx, writeOffID, adj)
			}); err != nil {
				t.Fatalf("PostBadDebt: %v", err)
			}
			var entries, lines int
			var dr, cr int64
			if err := pool.QueryRow(ctx, `SELECT count(DISTINCT e.id), count(l.id), coalesce(sum(l.debit_cents), 0), coalesce(sum(l.credit_cents), 0)
FROM gl.journal_entry e JOIN gl.journal_line l ON l.entry_id = e.id
WHERE e.source_type = 'write_off' AND e.source_id = $1::uuid`, writeOffID).Scan(&entries, &lines, &dr, &cr); err != nil {
				t.Fatal(err)
			}
			if entries != 1 || lines != 3 || dr != 59000 || cr != 59000 {
				t.Fatalf("entries %d lines %d dr %d cr %d; want 1, 3, 59000, 59000", entries, lines, dr, cr)
			}
		})
	}
}
