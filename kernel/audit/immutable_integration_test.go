//go:build integration

package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/migrations/migrationstest"
)

// immutableDB creates a database on COUNSEL_TEST_DB, applies the init migration, seeds one
// user and appends one audit event. It returns a connection to the new database.
func immutableDB(t *testing.T) *pgx.Conn {
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
	name := "audit_immutable_" + hex.EncodeToString(b)
	defer migrationstest.LockClusterRoles(ctx, t, admin)()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.Database = name
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	ddl, err := os.ReadFile(filepath.FromSlash("../../migrations/0001_init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.PgConn().Exec(ctx, string(ddl)).ReadAll(); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	var user string
	err = conn.QueryRow(ctx, `WITH r AS (INSERT INTO core.role(key, name, status) VALUES ('auditor', 'Auditor', 'active') RETURNING id)
INSERT INTO core.app_user(email, display_name, role_id) SELECT 'a@example.com', 'A', id FROM r RETURNING id::text`).Scan(&user)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	log := New(conn, nil, Options{})
	if err := log.Append(ctx, tx, Event{ActorKind: "user", ActorID: user, Action: "platform.test", Subject: Subject{Kind: "thing"}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return conn
}

// TestAuditEventImmutable checks that the audit_immutable and audit_no_truncate triggers
// refuse every change to a stored audit row, and that the refusal maps to ErrImmutable.
func TestAuditEventImmutable(t *testing.T) {
	conn := immutableDB(t)
	ctx := context.Background()
	tests := []struct {
		name string
		sql  string
	}{
		{"update", `UPDATE audit.audit_event SET action = 'platform.rewritten' WHERE seq = 1`},
		{"delete", `DELETE FROM audit.audit_event WHERE seq = 1`},
		{"truncate", `TRUNCATE audit.audit_event`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := conn.Exec(ctx, tt.sql)
			if err == nil {
				t.Fatalf("%s succeeded; want trigger refusal", tt.name)
			}
			if !errors.Is(mapErr(err), ErrImmutable) {
				t.Fatalf("%s: got %v, want ErrImmutable", tt.name, err)
			}
			var n int
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM audit.audit_event WHERE action = 'platform.test'`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("after %s: %d original rows, want 1", tt.name, n)
			}
		})
	}
}
