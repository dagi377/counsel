//go:build integration

package identity_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/migrations"
	"github.com/dagi377/counsel/migrations/migrationstest"
)

// breakGlassDB creates a fresh database on COUNSEL_TEST_DB with every migration applied and
// returns a pool on it.
func breakGlassDB(t *testing.T) *db.Pool {
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
	name := "identity_break_glass_" + hex.EncodeToString(b)
	defer migrationstest.LockClusterRoles(ctx, t, admin)()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	ccfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ccfg.Database = name
	conn, err := pgx.ConnectConfig(ctx, ccfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, conn, ""); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(ctx)
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pcfg.ConnConfig.Database = name
	pool, err := db.OpenConfig(ctx, pcfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestPGGrantsOnPostgres(t *testing.T) {
	pool := breakGlassDB(t)
	ctx := context.Background()
	// The kernel does not know the app's subject column; read it from the frozen table, the
	// one uuid column that is not the grant id, the user or the reviewer.
	var col string
	if err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT column_name FROM information_schema.columns
WHERE table_schema = 'audit' AND table_name = 'break_glass' AND data_type = 'uuid'
  AND column_name NOT IN ('id', 'user_id', 'reviewer_id')`).Scan(&col)
	}); err != nil {
		t.Fatalf("subject column: %v", err)
	}
	store := identity.PGGrants{DB: pool, SubjectKind: "subject", SubjectColumn: col}
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

	gs, err := store.List(ctx)
	if err != nil || len(gs) != 0 {
		t.Fatalf("List on empty table = %v, %v", gs, err)
	}
	if _, ok, err := store.Active(ctx, "00000000-0000-0000-0000-000000000001",
		identity.Subject{Kind: "subject", ID: "00000000-0000-0000-0000-000000000002"}, now); ok || err != nil {
		t.Fatalf("Active on empty table = %v, %v", ok, err)
	}
	err = pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		n, err := store.ExpireDue(ctx, tx, now, func(identity.BreakGlassGrant) error { return nil })
		if err == nil && n != 0 {
			t.Fatalf("ExpireDue on empty table = %d", n)
		}
		return err
	})
	if err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}

	// The table CHECK caps a grant at 8 hours, independently of the Go check.
	err = pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO audit.break_glass (user_id, `+pgx.Identifier{col}.Sanitize()+`, reason, explanation, starts_at, ends_at)
VALUES (core.uuidv7(), core.uuidv7(), 'r', 'e', $1, $2)`, now, now.Add(identity.BreakGlassMax+time.Minute))
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("over-8h insert err = %v, want check violation 23514", err)
	}
}
