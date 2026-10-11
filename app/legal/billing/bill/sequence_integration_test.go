//go:build integration

package bill

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const initDDLPath = "../../../../migrations/0001_init.sql"

// testPool creates a fresh database on the COUNSEL_TEST_DB server, applies 0001_init.sql
// and returns a pool on it. The database is dropped when the test ends.
func testPool(t *testing.T) *pgxpool.Pool {
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
	name := "bill_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse COUNSEL_TEST_DB: %v", err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 8
	ddl, err := os.ReadFile(initDDLPath)
	if err != nil {
		t.Fatalf("read %s: %v", initDDLPath, err)
	}
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	defer conn.Close(ctx)
	if _, err := conn.PgConn().Exec(ctx, string(ddl)).ReadAll(); err != nil {
		t.Fatalf("apply %s: %v", initDDLPath, err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedCounter sets the bill counter for prefix and year to next.
func seedCounter(t *testing.T, pool *pgxpool.Pool, prefix string, year, next int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO core.number_series (series, period_key, next_value) VALUES ($1, $2, $3)`,
		billSeries, fmt.Sprintf("%s-%04d", prefix, year), next); err != nil {
		t.Fatalf("seed counter: %v", err)
	}
}

// rule: BIL-C-120
func TestBIL_C_120_ConcurrentApprovalsGapless(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	seedCounter(t, pool, "INV", 2026, 147)

	// The first tx takes 0147 and holds the row lock; the second must wait, then get 0148.
	tx1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n1, err := NextNumber(ctx, tx1, "INV", 2026)
	if err != nil {
		t.Fatal(err)
	}
	var (
		wg sync.WaitGroup
		n2 string
		e2 error
	)
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		e2 = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			var err error
			n2, err = NextNumber(ctx, tx, "INV", 2026)
			return err
		})
	}()
	select {
	case <-done:
		t.Fatal("second approval did not wait for the counter lock")
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx1.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if e2 != nil {
		t.Fatal(e2)
	}
	got := []string{n1, n2}
	slices.Sort(got)
	if want := []string{"INV-2026-0147", "INV-2026-0148"}; !slices.Equal(got, want) {
		t.Fatalf("numbers = %v, want %v", got, want)
	}
}

// rule: BIL-C-120
func TestBIL_C_120_RollbackDoesNotConsume(t *testing.T) {
	tests := []struct {
		name string
		ops  []bool // true = commit, false = rollback
		want []string
	}{
		{"rollback then commit", []bool{false, true}, []string{"INV-2026-0001"}},
		{"commit rollback commit", []bool{true, false, true}, []string{"INV-2026-0001", "INV-2026-0002"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool := testPool(t)
			ctx := context.Background()
			var got []string
			for _, commit := range tc.ops {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				n, err := NextNumber(ctx, tx, "INV", 2026)
				if err != nil {
					t.Fatal(err)
				}
				if !commit {
					if err := tx.Rollback(ctx); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if err := tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				got = append(got, n)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("committed numbers = %v, want %v", got, tc.want)
			}
		})
	}
}
