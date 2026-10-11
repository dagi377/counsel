//go:build integration

package outbox_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/migrations/migrationstest"
	"github.com/jackc/pgx/v5/pgxpool"
)

const initDDL = "../../migrations/0001_init.sql"

// testDB creates a fresh database on COUNSEL_TEST_DB, applies the init migration and adds
// one table that stands for a consumer's effect.
func testDB(t *testing.T) *pgxpool.Pool {
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
	name := "outbox_relay_" + hex.EncodeToString(suffix)
	defer migrationstest.LockClusterRoles(ctx, t, admin)()
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
	ddl, err := os.ReadFile(initDDL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Conn().PgConn().Exec(ctx, string(ddl)).ReadAll(); err != nil {
		t.Fatalf("apply %s: %v", initDDL, err)
	}
	// No unique key on purpose: only the inbox keeps the effect single.
	if _, err := pool.Exec(ctx, `CREATE TABLE public.test_state (id uuid PRIMARY KEY);
CREATE TABLE public.test_effect (message_id uuid NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	return pool
}

type poolTx struct{ pool *pgxpool.Pool }

func (p poolTx) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error { return fn(ctx, tx) })
}

func count(t *testing.T, pool *pgxpool.Pool, sql string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

var errKilled = errors.New("relay killed")

// dyingRelay stops before the first MarkPublished, as a relay killed between the
// consumer's commit and the publish would.
type dyingRelay struct {
	outbox.Relay
	dead atomic.Bool
}

func (d *dyingRelay) MarkPublished(ctx context.Context, id string) error {
	if d.dead.CompareAndSwap(false, true) {
		return errKilled
	}
	return d.Relay.MarkPublished(ctx, id)
}

// countingInbox counts delivery attempts.
type countingInbox struct {
	outbox.Inbox
	attempts *atomic.Int32
}

func (c countingInbox) Claim(ctx context.Context, tx pgx.Tx, consumer, id string) (bool, error) {
	c.attempts.Add(1)
	return c.Inbox.Claim(ctx, tx, consumer, id)
}

func TestEmitInTxAndRelayExactlyOnceEffect(t *testing.T) {
	pool := testDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ob := outbox.NewPG(pool)
	db := poolTx{pool}

	tests := []struct {
		name     string
		rollback bool
		wantRows int
	}{
		{name: "rollback leaves no outbox row", rollback: true, wantRows: 0},
		{name: "commit writes exactly one outbox row", wantRows: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errRollback := errors.New("rollback")
			err := db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				var id string
				if err := tx.QueryRow(ctx, `INSERT INTO public.test_state (id) VALUES (core.uuidv7()) RETURNING id::text`).Scan(&id); err != nil {
					return err
				}
				if err := ob.Emit(ctx, tx, outbox.Subject{Kind: "test.state", ID: id}, "test.state_changed", map[string]any{"id": id}); err != nil {
					return err
				}
				if tt.rollback {
					return errRollback
				}
				return nil
			})
			if tt.rollback != errors.Is(err, errRollback) || (!tt.rollback && err != nil) {
				t.Fatalf("InTx = %v", err)
			}
			if n := count(t, pool, `SELECT count(*) FROM platform.outbox`); n != tt.wantRows {
				t.Fatalf("outbox rows = %d, want %d", n, tt.wantRows)
			}
			if n := count(t, pool, `SELECT count(*) FROM public.test_state`); n != tt.wantRows {
				t.Fatalf("state rows = %d, want %d", n, tt.wantRows)
			}
		})
	}

	var attempts atomic.Int32
	consumers := []outbox.Consumer{{
		Name: "test.effect",
		Handle: func(ctx context.Context, tx pgx.Tx, m outbox.Message) error {
			_, err := tx.Exec(ctx, `INSERT INTO public.test_effect (message_id) VALUES ($1)`, m.ID)
			return err
		},
	}}
	inbox := countingInbox{Inbox: ob, attempts: &attempts}

	// First relay: the consumer commits, then the relay dies before publishing.
	first := &outbox.Deliverer{Relay: &dyingRelay{Relay: ob}, Inbox: inbox, DB: db, Consumers: consumers}
	if _, err := first.Deliver(ctx, 10); !errors.Is(err, errKilled) {
		t.Fatalf("first Deliver = %v, want the relay killed", err)
	}
	// Restarted relay: delivers again; the inbox turns the repeat into a no-op.
	restarted := &outbox.Deliverer{Relay: ob, Inbox: inbox, DB: db, Consumers: consumers}
	n, err := restarted.Deliver(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("restarted Deliver = %d, %v; want 1, nil", n, err)
	}
	if n, err := restarted.Deliver(ctx, 10); err != nil || n != 0 {
		t.Fatalf("third Deliver = %d, %v; want nothing pending", n, err)
	}
	checks := []struct {
		name string
		got  int
		want int
	}{
		{name: "delivery attempts", got: int(attempts.Load()), want: 2},
		{name: "inbox rows", got: count(t, pool, `SELECT count(*) FROM platform.inbox`), want: 1},
		{name: "effect rows", got: count(t, pool, `SELECT count(*) FROM public.test_effect`), want: 1},
		{name: "unpublished rows", got: count(t, pool, `SELECT count(*) FROM platform.outbox WHERE published_at IS NULL`), want: 0},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}
