//go:build integration

package withdraw

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

// firmPool creates a migrated database on COUNSEL_TEST_DB, with no firm row, and returns a
// pool on it.
func firmPool(t *testing.T) *db.Pool {
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
	name := "withdraw_firm_" + hex.EncodeToString(b)
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

// The steps run in order on one database: no row first, then the row is created and changed.
func TestPGFirmSolePractitionerIntegration(t *testing.T) {
	pool := firmPool(t)
	ctx := context.Background()
	firm := NewPGFirm(pool)
	steps := []struct {
		name string
		sql  string
		want bool
		err  error
	}{
		{name: "no firm row fails closed", err: ErrNoFirm},
		{name: "default is not sole", sql: `INSERT INTO core.firm(legal_name, display_name) VALUES ('Firm LLP', 'Firm')`},
		{name: "set sole", sql: `UPDATE core.firm SET sole_practitioner = true`, want: true},
		{name: "unset sole", sql: `UPDATE core.firm SET sole_practitioner = false`},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			if s.sql != "" {
				if err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
					_, err := tx.Exec(ctx, s.sql)
					return err
				}); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}
			got, err := firm.SolePractitioner(ctx)
			if !errors.Is(err, s.err) {
				t.Fatalf("err = %v, want %v", err, s.err)
			}
			if got != s.want {
				t.Fatalf("solo = %v, want %v", got, s.want)
			}
		})
	}
}
