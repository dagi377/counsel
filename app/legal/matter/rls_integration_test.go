//go:build integration

package matter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/migrations/migrationstest"
)

// rlsDB creates a database on COUNSEL_TEST_DB and applies the init migration.
func rlsDB(t *testing.T) *pgx.Conn {
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
	name := "matter_rls_" + hex.EncodeToString(b)
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
	ddl, err := os.ReadFile(filepath.FromSlash("../../../migrations/0001_init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.PgConn().Exec(ctx, string(ddl)).ReadAll(); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	return conn
}

// rlsSeed is one firm_open matter with a note and a fact, a lawyer on the team and a
// screened user behind an active wall on that matter.
type rlsSeed struct {
	matter, lawyer, screened, outsider string
}

func seedRLS(ctx context.Context, t *testing.T, conn *pgx.Conn) rlsSeed {
	t.Helper()
	var s rlsSeed
	err := conn.QueryRow(ctx, `
WITH r AS (INSERT INTO core.role(key, name, status) VALUES ('lawyer', 'Lawyer', 'active') RETURNING id),
     u AS (INSERT INTO core.app_user(email, display_name, role_id, licence_type, lso_number, status, principal_kind)
           SELECT e, e, r.id, 'none', NULL, 'active', 'human' FROM r, unnest(ARRAY['a@x.test','d@x.test','o@x.test']) e
           RETURNING id, email)
SELECT (SELECT id FROM u WHERE email = 'a@x.test')::text,
       (SELECT id FROM u WHERE email = 'd@x.test')::text,
       (SELECT id FROM u WHERE email = 'o@x.test')::text`).Scan(&s.lawyer, &s.screened, &s.outsider)
	if err != nil {
		t.Fatalf("seed users: %v", err)
	}
	err = conn.QueryRow(ctx, `
WITH c AS (INSERT INTO party.contact(kind, legal_name, display_name, state) VALUES ('person', 'Client', 'Client', 'active') RETURNING id),
     pa AS (INSERT INTO matter.practice_area(name) VALUES ('General') RETURNING id)
INSERT INTO matter.matter(number, short_name, client_id, practice_area_id, status, open_date, responsible_lawyer_id, visibility)
SELECT 'M-1', 'Matter', c.id, pa.id, 'open', '2026-01-05', $1, 'firm_open' FROM c, pa
RETURNING id::text`, s.lawyer).Scan(&s.matter)
	if err != nil {
		t.Fatalf("seed matter: %v", err)
	}
	seeds := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO matter.note(matter_id, author_id, body_ref) VALUES ($1, $2, 'obj/1')`, []any{s.matter, s.lawyer}},
		{`INSERT INTO matter.fact(matter_id, key, value, created_by) VALUES ($1, 'k', '"v"', $2)`, []any{s.matter, s.lawyer}},
		{`WITH w AS (INSERT INTO matter.ethical_wall(reason, status, effective_from, created_by)
		            VALUES ('conflict', 'active', now(), $2) RETURNING id),
		      wm AS (INSERT INTO matter.ethical_wall_matter(wall_id, matter_id) SELECT id, $1 FROM w)
		 INSERT INTO matter.ethical_wall_screened_user(wall_id, user_id) SELECT id, $3 FROM w`, []any{s.matter, s.lawyer, s.screened}},
	}
	for _, sd := range seeds {
		if _, err := conn.Exec(ctx, sd.sql, sd.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return s
}

// countAs counts rows of tbl for the matter as viewer under the app role, inside one tx.
func countAs(ctx context.Context, t *testing.T, conn *pgx.Conn, viewer, tbl, matterCol, matterID string) int {
	t.Helper()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config($1, $2, true)", SessionUserVar, viewer); err != nil {
		t.Fatalf("bind user: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+AppRole); err != nil {
		t.Fatalf("set role: %v", err)
	}
	var n int
	q := "SELECT count(*) FROM " + tbl + " WHERE " + matterCol + " = $1"
	if err := tx.QueryRow(ctx, q, matterID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", tbl, err)
	}
	return n
}

func TestMatterRLSWalledUserSeesNoRows(t *testing.T) {
	conn := rlsDB(t)
	ctx := context.Background()
	if err := CheckRLS(ctx, conn); err != nil {
		t.Fatalf("CheckRLS: %v", err)
	}
	s := seedRLS(ctx, t, conn)
	tables := []struct {
		tbl, col string
	}{
		{"matter.matter", "id"},
		{"matter.note", "matter_id"},
		{"matter.fact", "matter_id"},
	}
	viewers := []struct {
		name, id string
		want     int
	}{
		{"walled", s.screened, 0},
		{"responsible lawyer", s.lawyer, 1},
		{"unscreened firm user", s.outsider, 1},
	}
	for _, tb := range tables {
		for _, v := range viewers {
			t.Run(tb.tbl+"/"+v.name, func(t *testing.T) {
				if got := countAs(ctx, t, conn, v.id, tb.tbl, tb.col, s.matter); got != v.want {
					t.Fatalf("rows = %d, want %d", got, v.want)
				}
			})
		}
	}
}

func TestCheckRLSFailsClosedWhenNotForced(t *testing.T) {
	conn := rlsDB(t)
	ctx := context.Background()
	if _, err := conn.Exec(ctx, "ALTER TABLE matter.note NO FORCE ROW LEVEL SECURITY"); err != nil {
		t.Fatalf("alter: %v", err)
	}
	if err := CheckRLS(ctx, conn); err == nil {
		t.Fatal("CheckRLS passed with RLS not forced on matter.note")
	}
}
