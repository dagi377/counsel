//go:build integration

package boot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// probeArgs is a job the insert-only client queues and the worker role runs.
type probeArgs struct {
	N int `json:"n"`
}

func (probeArgs) Kind() string { return "insert_probe" }

type probeWorker struct {
	river.WorkerDefaults[probeArgs]
	got chan int
}

func (w *probeWorker) Work(_ context.Context, j *river.Job[probeArgs]) error {
	w.got <- j.Args.N
	return nil
}

// riverDB creates a fresh database on COUNSEL_TEST_DB with River's tables.
func riverDB(t *testing.T) *pgxpool.Pool {
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
	name := "boot_insert_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatalf("river migrate: %v", err)
	}
	return pool
}

// TestInsertClientJobsRunOnWorker inserts through the insert-only client, as counseld does,
// and checks the worker role's client works each job once.
func TestInsertClientJobsRunOnWorker(t *testing.T) {
	pool := riverDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ins, err := newInsertClient(pool)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		n    int
	}{
		{"first job", 1},
		{"second job", 2},
	}
	for _, tc := range tests {
		if _, err := ins.Insert(ctx, probeArgs{N: tc.n}, nil); err != nil {
			t.Fatalf("%s: insert: %v", tc.name, err)
		}
	}
	j := NewJobs()
	w := &probeWorker{got: make(chan int, len(tests))}
	if err := AddWorker[probeArgs](j, w); err != nil {
		t.Fatal(err)
	}
	client, err := newRiverClient(pool, WorkerConfig{MaxWorkers: 2}.riverConfig(j))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Stop(context.Background()) }()
	seen := map[int]bool{}
	for range tests {
		select {
		case n := <-w.got:
			seen[n] = true
		case <-ctx.Done():
			t.Fatalf("worker ran %d of %d jobs", len(seen), len(tests))
		}
	}
	for _, tc := range tests {
		if !seen[tc.n] {
			t.Errorf("%s: not worked", tc.name)
		}
	}
}
