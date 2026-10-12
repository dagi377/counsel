//go:build integration

package boot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// freshDB creates an empty database on COUNSEL_TEST_DB and returns its DSN.
func freshDB(t *testing.T) string {
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
	name := "boot_river_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("COUNSEL_TEST_DB is not a URL: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

// rolePool opens a pool on dsn whose sessions run as role, so the test sees only that
// role's grants.
func rolePool(t *testing.T, dsn, role string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize())
		return err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestMigrateCreatesRiverSchema runs counsel-migrate's expand step on a fresh database
// (twice, as a retried Job would), then queues a job as counsel_app, as counseld does, and
// works it as counsel_jobs, as counsel-worker does.
// rule: MIG-060
func TestMigrateCreatesRiverSchema(t *testing.T) {
	dsn := freshDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for i := range 2 {
		if err := pgApply(ctx, dsn, MigrateExpand); err != nil {
			t.Fatalf("expand run %d: %v", i+1, err)
		}
	}
	tests := []struct {
		name string
		n    int
	}{
		{"first job", 1},
		{"second job", 2},
	}
	ins, err := newInsertClient(rolePool(t, dsn, "counsel_app"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		if _, err := ins.Insert(ctx, probeArgs{N: tc.n}, nil); err != nil {
			t.Fatalf("%s: insert as counsel_app: %v", tc.name, err)
		}
	}
	j := NewJobs()
	w := &probeWorker{got: make(chan int, len(tests))}
	if err := AddWorker[probeArgs](j, w); err != nil {
		t.Fatal(err)
	}
	client, err := newRiverClient(rolePool(t, dsn, "counsel_jobs"), WorkerConfig{MaxWorkers: 2}.riverConfig(j))
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
