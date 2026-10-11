//go:build integration

package ledger

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/migrations"
	"github.com/jackc/pgx/v5"
)

// freshDB creates a database on COUNSEL_TEST_DB, applies every migration and seeds one
// user and a small chart. It returns a connection and the user id.
func freshDB(t *testing.T) (*pgx.Conn, string) {
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
	name := "ledger_post_" + hex.EncodeToString(b)
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
	if err := migrations.Apply(ctx, conn, ""); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	var user string
	err = conn.QueryRow(ctx, `WITH r AS (INSERT INTO core.role(key, name, status) VALUES ('poster', 'Poster', 'active') RETURNING id)
INSERT INTO core.app_user(email, display_name, role_id) SELECT 'p@example.com', 'P', id FROM r RETURNING id::text`).Scan(&user)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	for _, a := range []struct{ code, typ string }{{"1000", "asset"}, {"1100", "asset"}, {"2000", "liability"}, {"4000", "revenue"}, {"6000", "expense"}} {
		if _, err := conn.Exec(ctx, `INSERT INTO gl.account(code, name, type) VALUES ($1, $1, $2)`, a.code, a.typ); err != nil {
			t.Fatalf("seed account: %v", err)
		}
	}
	return conn, user
}

func uuidish(n int) string { return fmt.Sprintf("00000000-0000-7000-8000-%012d", n) }

// rule: BIL-I-001
func TestBIL_I_001_UnbalancedInsertFailsAtCommit(t *testing.T) {
	conn, user := freshDB(t)
	ctx := context.Background()
	tests := []struct {
		name   string
		debit  int64
		credit int64
		lines  int
	}{
		{"unbalanced", 500, 499, 2},
		{"single line", 500, 0, 1},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO gl.journal_entry(entry_date, memo, source_type, source_id, posted_by)
VALUES ('2026-03-31', 't', 'manual', $1::uuid, $2::uuid) RETURNING id::text`, uuidish(900+i), user).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO gl.journal_line(entry_id, account_id, debit_cents) SELECT $1::uuid, id, $2 FROM gl.account WHERE code='1000'`, id, tt.debit); err != nil {
				t.Fatal(err)
			}
			if tt.lines > 1 {
				if _, err := tx.Exec(ctx, `INSERT INTO gl.journal_line(entry_id, account_id, credit_cents) SELECT $1::uuid, id, $2 FROM gl.account WHERE code='4000'`, id, tt.credit); err != nil {
					t.Fatal(err)
				}
			}
			err = tx.Commit(ctx)
			if err == nil || !strings.Contains(err.Error(), "unbalanced") {
				t.Fatalf("commit = %v, want journal_balanced error", err)
			}
		})
	}
}

func TestPostPropertyTrialBalanceAndIdempotency(t *testing.T) {
	conn, user := freshDB(t)
	ctx := context.Background()
	g := &PG{PostedBy: func(context.Context) (string, error) { return user, nil }}
	r := mrand.New(mrand.NewPCG(7, 11))
	accts := []string{"1000", "1100", "2000", "4000", "6000"}
	const n = 60
	ids := make([]EntryID, n)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]Entry, n)
	for i := range n {
		e := randomEntry(r, accts, i)
		e.SourceID = uuidish(i + 1)
		entries[i] = e
		if ids[i], err = g.Post(ctx, tx, e); err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range entries {
		again, err := g.Post(ctx, tx, e)
		if err != nil || again != ids[i] {
			t.Fatalf("repost %d = %s, %v; want %s", i, again, err, ids[i])
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var count int
	var sum int64
	if err := conn.QueryRow(ctx, `SELECT count(DISTINCT entry_id), coalesce(sum(debit_cents - credit_cents), 0) FROM gl.journal_line`).Scan(&count, &sum); err != nil {
		t.Fatal(err)
	}
	if count != n || sum != 0 {
		t.Fatalf("entries = %d, trial balance = %d; want %d, 0", count, sum, n)
	}
}

func TestPostUnknownAccount(t *testing.T) {
	conn, user := freshDB(t)
	ctx := context.Background()
	g := &PG{PostedBy: func(context.Context) (string, error) { return user, nil }}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	e := Entry{Book: "main", Date: Date{Year: 2026, Month: 3, Day: 1}, SourceKind: "manual", SourceID: uuidish(1),
		Lines: []Line{{Account: "1000", Side: Debit, Amount: 5}, {Account: "9999", Side: Credit, Amount: 5}}}
	if _, err := g.Post(ctx, tx, e); !errors.Is(err, ErrUnknownAccount) {
		t.Fatalf("Post = %v, want ErrUnknownAccount", err)
	}
}

// rule: BIL-I-003
func TestBIL_I_003_JournalRowsAreAppendOnly(t *testing.T) {
	conn, user := freshDB(t)
	ctx := context.Background()
	g := &PG{PostedBy: func(context.Context) (string, error) { return user, nil }}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	e := Entry{Book: "main", Date: Date{Year: 2026, Month: 3, Day: 1}, SourceKind: "manual", SourceID: uuidish(1),
		Lines: []Line{{Account: "1000", Side: Debit, Amount: 5}, {Account: "4000", Side: Credit, Amount: 5}}}
	if _, err := g.Post(ctx, tx, e); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`UPDATE gl.journal_line SET debit_cents = debit_cents`,
		`DELETE FROM gl.journal_line`,
		`UPDATE gl.journal_entry SET memo = 'x'`,
		`DELETE FROM gl.journal_entry`,
	} {
		t.Run(stmt, func(t *testing.T) {
			if _, err := conn.Exec(ctx, stmt); err == nil {
				t.Fatalf("%s succeeded, want error", stmt)
			}
		})
	}
}
