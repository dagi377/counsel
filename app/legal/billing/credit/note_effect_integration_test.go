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

	"github.com/dagi377/counsel/migrations"
	"github.com/dagi377/counsel/migrations/migrationstest"
)

// noteTestConn creates a fresh database on the COUNSEL_TEST_DB server, applies every
// migration and returns a connection to it. The database is dropped when the test ends.
func noteTestConn(t *testing.T) *pgx.Conn {
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
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("random name: %v", err)
	}
	name := "credit_note_" + hex.EncodeToString(suffix)
	defer migrationstest.LockClusterRoles(ctx, t, admin)()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse COUNSEL_TEST_DB: %v", err)
	}
	cfg.Database = name
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		conn.Close(ctx)
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})
	if err := migrations.Apply(ctx, conn, ""); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return conn
}

// TestPGNotesMissingRows checks PGNotes against the real billing.credit_note schema: unknown
// rows are not found and an unknown note is never issued.
func TestPGNotesMissingRows(t *testing.T) {
	conn := noteTestConn(t)
	ctx := context.Background()
	const missing = "00000000-0000-7000-8000-000000000001"
	tests := []struct {
		name string
		run  func(tx pgx.Tx) error
		want error
	}{
		{"lock unknown note", func(tx pgx.Tx) error { _, err := (PGNotes{}).Lock(ctx, tx, missing); return err }, ErrNotFound},
		{"bucket of unknown bill", func(tx pgx.Tx) error { _, err := (PGNotes{}).Bucket(ctx, tx, missing, missing); return err }, ErrNotFound},
		{"issue unknown note", func(tx pgx.Tx) error {
			return (PGNotes{}).Issue(ctx, tx, missing, "CN-1", missing, time.Now())
		}, ErrInvalidState},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := tt.run(tx); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
