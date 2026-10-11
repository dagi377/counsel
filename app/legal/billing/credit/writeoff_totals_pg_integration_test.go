//go:build integration

package credit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/migrations"
)

// writeOffTotalsDB creates a migrated database on COUNSEL_TEST_DB and returns a pool on it.
func writeOffTotalsDB(t *testing.T) *db.Pool {
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
	name := "credit_writeoff_totals_" + hex.EncodeToString(b)
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
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	err = migrations.Apply(ctx, conn, "")
	_ = conn.Close(ctx)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	pool, err := db.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Seeding a bill needs most of the schema; the scripted-tx tests cover the sums. This test
// checks every statement against the migrated schema and the not-found paths.
func TestPGWriteOffTotalsIntegration(t *testing.T) {
	pool := writeOffTotalsDB(t)
	ctx := context.Background()
	since := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	statements := []struct {
		name string
		sql  string
		args []any
	}{
		{"lock bill", lockBillSQL, []any{"00000000-0000-7000-8000-000000000001"}},
		{"client bills", clientBillsSQL, []any{"00000000-0000-7000-8000-000000000002"}},
		{"approved", approvedWriteOffsSQL, []any{"00000000-0000-7000-8000-000000000001", []string{}, since}},
		{"pending", pendingWriteOffsSQL, []any{SubjectWriteOff}},
	}
	for _, st := range statements {
		t.Run(st.name, func(t *testing.T) {
			err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				rows, err := tx.Query(ctx, st.sql, st.args...)
				if err != nil {
					return err
				}
				rows.Close()
				return rows.Err()
			})
			if err != nil {
				t.Fatalf("%s: %v", st.name, err)
			}
		})
	}
	tests := []struct {
		name string
		bill string
	}{
		{"unknown bill", "00000000-0000-7000-8000-000000000099"},
		{"malformed bill id", "not-a-uuid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				_, err := PGWriteOffTotals{}.CumulativeCents(ctx, tx, WriteOffScope{BillID: tt.bill, ClientID: "c1", Since: since})
				return err
			})
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}
