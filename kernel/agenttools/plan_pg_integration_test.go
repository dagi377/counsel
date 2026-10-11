//go:build integration

package agenttools_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/migrations"
	"github.com/dagi377/counsel/migrations/migrationstest"
)

// planDBOnPostgres creates a fresh database on COUNSEL_TEST_DB with every migration applied
// and returns a pool on it.
func planDBOnPostgres(t *testing.T) *db.Pool {
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
	name := "agent_plan_" + hex.EncodeToString(b)
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

func TestPGPlansOnPostgres(t *testing.T) {
	pool := planDBOnPostgres(t)
	ctx := context.Background()
	store := agenttools.PGPlans{DB: pool}
	pl := agenttools.NewPlanner(store)

	p, err := pl.ReportPlan(ctx, "run-1", []string{"a", "b"})
	if err != nil {
		t.Fatalf("ReportPlan: %v", err)
	}
	if _, err := pl.RecordCall(ctx, "run-1", "c1"); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}
	if err := pl.ReportStep(ctx, p.ID, 0, agenttools.StepDone); err != nil {
		t.Fatalf("ReportStep: %v", err)
	}
	got, at, err := agenttools.NewPlanner(store).Current(ctx, "run-1")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	want := agenttools.Plan{ID: p.ID, RunID: "run-1", Steps: []agenttools.Step{
		{Index: 0, Title: "a", Status: agenttools.StepDone, CallIDs: []string{"c1"}},
		{Index: 1, Title: "b", Status: agenttools.StepPending},
	}}
	if !reflect.DeepEqual(got, want) || at != 1 {
		t.Fatalf("Current = %+v at %d, want %+v at 1", got, at, want)
	}

	// New titles replace the run's plan: still one row per run.
	np, err := pl.ReportPlan(ctx, "run-1", []string{"x"})
	if err != nil {
		t.Fatalf("ReportPlan again: %v", err)
	}
	var n int
	if err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM agent.plan WHERE run_id = 'run-1'`).Scan(&n)
	}); err != nil || n != 1 {
		t.Fatalf("rows for run = %d, %v; want 1", n, err)
	}
	if got, err := store.ByID(ctx, np.ID); err != nil || len(got.Steps) != 1 {
		t.Fatalf("ByID new plan = %+v, %v", got, err)
	}
	if _, err := store.ByRun(ctx, "run-2"); !errors.Is(err, agenttools.ErrNotFound) {
		t.Fatalf("missing run err = %v, want ErrNotFound", err)
	}
}
