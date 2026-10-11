//go:build integration

package work_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/work"
	"github.com/dagi377/counsel/kernel/work/worktest"
	"github.com/dagi377/counsel/migrations"
	"github.com/dagi377/counsel/migrations/migrationstest"
)

const testKind = "desk.ticket"

// workDB creates a throwaway database with every migration applied.
func workDB(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "work_pg_" + hex.EncodeToString(suffix)
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
		t.Fatalf("apply migrations: %v", err)
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

// seed inserts one workflow (todo, doing, done; todo -> doing) and three users.
func seed(t *testing.T, pool *db.Pool) worktest.Env {
	t.Helper()
	e := worktest.Env{SubjectKind: testKind}
	err := pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO work.workflow (name, version) VALUES ('wf', 1) RETURNING id::text`).Scan(&e.WorkflowID); err != nil {
			return err
		}
		for _, s := range []struct {
			name, group string
			id          *string
		}{{"todo", "todo", &e.Todo}, {"doing", "in_progress", &e.Doing}, {"done", "done", &e.Done}} {
			if err := tx.QueryRow(ctx, `INSERT INTO work.status (workflow_id, name, status_group) VALUES ($1::uuid, $2, $3) RETURNING id::text`,
				e.WorkflowID, s.name, s.group).Scan(s.id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO work.transition (workflow_id, from_status_id, to_status_id, name) VALUES ($1::uuid, $2::uuid, $3::uuid, 'start')`,
			e.WorkflowID, e.Todo, e.Doing); err != nil {
			return err
		}
		var role string
		if err := tx.QueryRow(ctx, `INSERT INTO core.role (key, name, status) VALUES ('worker', 'Worker', 'active') RETURNING id::text`).Scan(&role); err != nil {
			return err
		}
		for i := range e.Users {
			if err := tx.QueryRow(ctx, `INSERT INTO core.app_user (email, display_name, role_id) VALUES ($1, 'U', $2::uuid) RETURNING id::text`,
				"u"+string(rune('0'+i))+"@example.com", role).Scan(&e.Users[i]); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `SELECT core.uuidv7()::text, core.uuidv7()::text, core.uuidv7()::text`).
			Scan(&e.SubjectIDs[0], &e.SubjectIDs[1], (*string)(&e.MissingID))
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return e
}

// TestPGContract runs the shared Service contract (also run by workfake) on Postgres.
func TestPGContract(t *testing.T) {
	kinds := work.NewSubjectKinds([]registry.Call{{Method: registry.MethodSubjectKind, Args: []any{registry.SubjectKind{ID: testKind}}}})
	worktest.Run(t, func(t *testing.T) worktest.Env {
		pool := workDB(t)
		e := seed(t, pool)
		e.Svc = &work.PG{DB: pool, Kinds: kinds}
		e.InTx = func(ctx context.Context, fn func(pgx.Tx) error) error {
			return pool.InTx(ctx, func(_ context.Context, tx pgx.Tx) error { return fn(tx) })
		}
		return e
	})
}
