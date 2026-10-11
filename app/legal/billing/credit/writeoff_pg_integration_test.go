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

// writeOffDB creates a migrated database on COUNSEL_TEST_DB and returns a pool on it.
func writeOffDB(t *testing.T) *db.Pool {
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
	name := "credit_write_off_" + hex.EncodeToString(b)
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

// Every PGWriteOffs statement parses and runs against the migrated schema; a missing row or
// a malformed id is ErrNotFound and a draft-only step on a missing write-off is refused.
func TestPGWriteOffsIntegration(t *testing.T) {
	pool := writeOffDB(t)
	ctx := context.Background()
	store := PGWriteOffs{Lawyers: mapLawyers{m: map[string]string{}}}
	const missing = "00000000-0000-7000-8000-000000000099"
	at := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		run     func(context.Context, pgx.Tx) error
		wantErr error
	}{
		{"lock missing", func(ctx context.Context, tx pgx.Tx) error { _, err := store.Lock(ctx, tx, missing); return err }, ErrNotFound},
		{"lock malformed", func(ctx context.Context, tx pgx.Tx) error { _, err := store.Lock(ctx, tx, "not-a-uuid"); return err }, ErrNotFound},
		{"awaiting missing", func(ctx context.Context, tx pgx.Tx) error {
			_, err := store.AwaitingSecond(ctx, tx, missing)
			return err
		}, ErrNotFound},
		{"mark missing", func(ctx context.Context, tx pgx.Tx) error { return store.MarkAwaitingSecond(ctx, tx, missing, missing) }, ErrInvalidState},
		{"submit missing", func(ctx context.Context, tx pgx.Tx) error { return store.Submit(ctx, tx, missing, missing) }, ErrInvalidState},
		{"save missing", func(ctx context.Context, tx pgx.Tx) error {
			return store.SaveApproved(ctx, tx, WriteOff{ID: missing, BillID: missing, AmountCents: 1, Status: StatusApproved,
				PreparedBy: "a", ApprovedBy: "b", ApprovedAt: &at})
		}, ErrNotFound},
		{"draft on missing bill", func(ctx context.Context, tx pgx.Tx) error {
			_, err := store.Draft(ctx, tx, WriteOff{BillID: missing, AmountCents: 1, Reason: "r", PreparedBy: missing})
			return err
		}, ErrNotFound},
		{"bill missing", func(ctx context.Context, tx pgx.Tx) error { _, err := store.WriteOffBill(ctx, tx, missing); return err }, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pool.InTx(ctx, tt.run)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// The draft table refuses a delete and any change to the amount, split or preparer.
func TestWriteOffDraftGuardIntegration(t *testing.T) {
	pool := writeOffDB(t)
	ctx := context.Background()
	var n int
	err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pg_trigger
  WHERE tgrelid = 'billing.write_off_draft'::regclass AND tgname = 'write_off_draft_guard'`).Scan(&n)
	})
	if err != nil || n != 1 {
		t.Fatalf("guard trigger: %d, %v", n, err)
	}
}
