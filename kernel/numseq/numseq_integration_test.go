//go:build integration

package numseq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

const initDDLPath = "../../migrations/0001_init.sql"

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
	name := "numseq_" + hex.EncodeToString(suffix)
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
	cfg.MaxConns = 16
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

func TestNextGaplessUnderConcurrency(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	clk := clockfake.New(time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC), nil)
	seq := New(clk)
	if err := seq.Define(ctx, Series{Name: "approval", Format: "{YYYY}-{SEQ:4}"}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		workers   int
		rollbacks int // the first rollbacks workers roll back after taking a number
		wantFirst string
		wantLast  string
	}{
		{"50 concurrent approvals", 50, 0, "2026-0001", "2026-0050"},
		{"rollbacks do not consume", 20, 10, "2026-0051", "2026-0060"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				mu  sync.Mutex
				got []string
				wg  sync.WaitGroup
			)
			errs := make(chan error, tt.workers)
			for i := 0; i < tt.workers; i++ {
				wg.Add(1)
				go func(rollback bool) {
					defer wg.Done()
					err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
						n, err := seq.Next(ctx, tx, "approval")
						if err != nil {
							return err
						}
						if rollback {
							return errRollback
						}
						mu.Lock()
						got = append(got, n)
						mu.Unlock()
						return nil
					})
					if err != nil && !errors.Is(err, errRollback) {
						errs <- err
					}
				}(i < tt.rollbacks)
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Fatalf("Next: %v", err)
			}
			slices.Sort(got)
			want := tt.workers - tt.rollbacks
			if len(got) != want || got[0] != tt.wantFirst || got[len(got)-1] != tt.wantLast {
				t.Fatalf("got %d numbers %v, want %d from %s to %s", len(got), got, want, tt.wantFirst, tt.wantLast)
			}
			for i := 1; i < len(got); i++ {
				if got[i] == got[i-1] {
					t.Fatalf("duplicate number %s", got[i])
				}
			}
		})
	}
}

var errRollback = errors.New("rollback")

func TestNextSequentialRollback(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	clk := clockfake.New(time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC), nil)
	seq := New(clk)
	for _, s := range []Series{{"yearly", "{YYYY}-{SEQ:4}"}, {"flat", "R{SEQ:3}"}} {
		if err := seq.Define(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	next := func(series string, commit bool) string {
		t.Helper()
		var n string
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			var err error
			if n, err = seq.Next(ctx, tx, series); err != nil {
				return err
			}
			if !commit {
				return errRollback
			}
			return nil
		})
		if err != nil && !errors.Is(err, errRollback) {
			t.Fatalf("Next(%s): %v", series, err)
		}
		return n
	}
	tests := []struct {
		name    string
		series  string
		commit  bool
		advance time.Duration
		want    string
	}{
		{"first", "yearly", true, 0, "2026-0001"},
		{"rolled back", "yearly", false, 0, "2026-0002"},
		{"reused after rollback", "yearly", true, 0, "2026-0002"},
		{"flat first", "flat", true, 0, "R001"},
		{"new business year restarts", "yearly", true, 24 * time.Hour, "2027-0001"},
		{"flat keeps counting", "flat", true, 0, "R002"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk.Advance(tt.advance)
			if got := next(tt.series, tt.commit); got != tt.want {
				t.Fatalf("Next = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRender(t *testing.T) {
	tests := []struct {
		name   string
		format string
		year   int
		n      int64
		want   string
	}{
		{"year and seq", "{YYYY}-{SEQ:4}", 2026, 1, "2026-0001"},
		{"seq only", "R{SEQ:6}", 2026, 42, "R000042"},
		{"overflow width", "{SEQ:2}", 2026, 123, "123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := render(tt.format, tt.year, tt.n); got != tt.want {
				t.Fatalf("render = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefine(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		series  []Series
		wantErr error
	}{
		{"ok", []Series{{"a", "{YYYY}-{SEQ:4}"}}, nil},
		{"same twice", []Series{{"a", "{SEQ:4}"}, {"a", "{SEQ:4}"}}, nil},
		{"conflict", []Series{{"a", "{SEQ:4}"}, {"a", "{SEQ:5}"}}, ErrConflict},
		{"no seq", []Series{{"a", "{YYYY}"}}, ErrBadFormat},
		{"two seq", []Series{{"a", "{SEQ:2}{SEQ:2}"}}, ErrBadFormat},
		{"unknown token", []Series{{"a", "{MM}{SEQ:2}"}}, ErrBadFormat},
		{"zero width", []Series{{"a", "{SEQ:0}"}}, ErrBadFormat},
		{"empty name", []Series{{"", "{SEQ:2}"}}, ErrBadFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(clockfake.New(time.Now(), nil))
			var err error
			for _, s := range tt.series {
				if err = p.Define(ctx, s); err != nil {
					break
				}
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Define err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNextUnknownSeries(t *testing.T) {
	p := New(clockfake.New(time.Now(), nil))
	if _, err := p.Next(context.Background(), nil, "missing"); !errors.Is(err, ErrUnknownSeries) {
		t.Fatalf("Next err = %v, want ErrUnknownSeries", err)
	}
}
