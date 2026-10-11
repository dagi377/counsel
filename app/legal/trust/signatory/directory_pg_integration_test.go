//go:build integration

package signatory

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

// directoryDB creates a migrated database on COUNSEL_TEST_DB and returns a pool on it.
func directoryDB(t *testing.T) *db.Pool {
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
	name := "signatory_directory_" + hex.EncodeToString(b)
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

func TestPGDirectoryUserIntegration(t *testing.T) {
	pool := directoryDB(t)
	ctx := context.Background()
	seed := []struct {
		key, sql string
	}{
		{"lawyer", `'lawyer', 'LSO1', 'good_standing', 'human', 'active', NULL, NULL`},
		{"paralegal", `'paralegal', 'LSO2', 'good_standing', 'human', 'active', NULL, NULL`},
		{"suspended", `'lawyer', 'LSO3', 'suspended', 'human', 'active', NULL, NULL`},
		{"left", `'lawyer', 'LSO4', 'good_standing', 'human', 'offboarded', now(), NULL`},
		{"expired", `'lawyer', 'LSO5', 'good_standing', 'human', 'active', NULL, DATE '2000-01-01'`},
		{"clerk", `'none', NULL, 'n/a', 'human', 'active', NULL, NULL`},
		{"agent", `'none', NULL, 'n/a', 'agent', 'active', NULL, NULL`},
	}
	ids := map[string]string{}
	err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ('staff', 'Staff', 'active') RETURNING id::text`).Scan(&role); err != nil {
			return err
		}
		for _, s := range seed {
			var id string
			q := `INSERT INTO core.app_user(email, display_name, role_id, licence_type, lso_number, licence_status,
  principal_kind, status, offboarded_at, expires_on)
VALUES ($1 || '@example.com', $1, $2::uuid, ` + s.sql + `) RETURNING id::text`
			if err := tx.QueryRow(ctx, q, s.key, role).Scan(&id); err != nil {
				return err
			}
			ids[s.key] = id
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	dir := NewPGDirectory(pool)
	tests := []struct {
		name     string
		id       string
		licensee bool
		active   bool
		err      error
	}{
		{name: "lawyer in good standing", id: ids["lawyer"], licensee: true, active: true},
		{name: "paralegal in good standing", id: ids["paralegal"], licensee: true, active: true},
		{name: "suspended licence", id: ids["suspended"], active: true},
		{name: "offboarded", id: ids["left"], licensee: true},
		{name: "expired account", id: ids["expired"], licensee: true},
		{name: "non-licensee", id: ids["clerk"], active: true},
		{name: "agent principal", id: ids["agent"], active: true},
		{name: "unknown id", id: "00000000-0000-7000-8000-000000000099", err: ErrNotFound},
		{name: "malformed id", id: "not-a-uuid", err: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := dir.User(ctx, tt.id)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if tt.err != nil {
				return
			}
			want := User{ID: tt.id, Licensee: tt.licensee, Active: tt.active}
			if u != want {
				t.Fatalf("user = %+v, want %+v", u, want)
			}
		})
	}
}
