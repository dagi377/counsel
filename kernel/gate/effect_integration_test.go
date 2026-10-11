//go:build integration

package gate_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/migrations"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/migrations/migrationstest"
	"github.com/jackc/pgx/v5/pgxpool"
)

// effectDB creates a fresh database on COUNSEL_TEST_DB with every migration applied.
func effectDB(t *testing.T) *pgxpool.Pool {
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
	name := "gate_effect_" + hex.EncodeToString(suffix)
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
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if err := migrations.Apply(ctx, conn.Conn(), ""); err != nil {
		t.Fatal(err)
	}
	return pool
}

func addUsers(t *testing.T, ctx context.Context, pool *pgxpool.Pool, emails ...string) []string {
	t.Helper()
	var role string
	if err := pool.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ('approver', 'Approver', 'active') RETURNING id::text`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(emails))
	for i, e := range emails {
		if err := pool.QueryRow(ctx, `
INSERT INTO core.app_user(email, display_name, role_id, status) VALUES ($1, $1, $2, 'active') RETURNING id::text`,
			e, role).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

// runOnce is one relay delivery: RunEffect in its own transaction.
func runOnce(ctx context.Context, pool *pgxpool.Pool, e *gate.PG, id gate.GateID) (ran bool, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		ran, err = e.RunEffect(ctx, tx, id)
		return err
	})
	return ran, err
}

// Relay delivery is at least once: the effect of one approved gate delivered three times
// at once runs exactly once (RV2-004).
func TestEffectRunsExactlyOnce(t *testing.T) {
	pool := effectDB(t)
	clk, err := clock.New("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	ctx := clock.WithClock(context.Background(), clk)
	users := addUsers(t, ctx, pool, "preparer@example.com", "approver@example.com")
	preparer, approver := users[0], users[1]

	var calls atomic.Int32
	e := gate.NewPG()
	// A cooling-off keeps Decide from running the effect, so only the relay runs it.
	k := gate.GateKind{
		Slug: "desk.refund", Approver: "desk.approve", Risk: gate.RiskMedium, MinApprovers: 1,
		TTL: 24 * time.Hour, CoolingOff: time.Hour, PayloadSchema: []byte(`{"type":"object"}`),
		Effect: func(context.Context, pgx.Tx, gate.Approved) error {
			calls.Add(1)
			time.Sleep(50 * time.Millisecond) // widen the race window
			return nil
		},
	}
	if err := e.Register(k); err != nil {
		t.Fatal(err)
	}
	if err := e.Kinds().Label(k.Slug, "G1"); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"amount_cents": 1250}`)
	hash, err := gate.PayloadHash(payload)
	if err != nil {
		t.Fatal(err)
	}
	subject := gate.Subject{Kind: "desk.refund_request", ID: "0190d0f4-0000-7000-8000-000000000001"}
	var id gate.GateID
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if id, err = e.Open(ctx, tx, k.Slug, subject, payload, gate.Principal{Kind: gate.PrincipalUser, UserID: preparer}); err != nil {
			return err
		}
		return e.Decide(ctx, tx, id, 1, gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: hash, MFAVerifiedAt: time.Now()},
			gate.Principal{Kind: gate.PrincipalUser, UserID: approver})
	})
	if err != nil {
		t.Fatalf("open and approve: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("Decide ran the effect during cooling-off")
	}

	// The relay delivers after the cooling-off; one delivery before it is a no-op.
	early, err := runOnce(ctx, pool, e, id)
	if err != nil || early {
		t.Fatalf("delivery during cooling-off: ran %v, err %v; want no run", early, err)
	}
	later := clock.WithClock(context.Background(), clockfake.New(time.Now().Add(2*time.Hour), clk.Location()))

	const deliveries = 3
	var (
		wg   sync.WaitGroup
		ran  atomic.Int32
		errs = make(chan error, deliveries)
	)
	for range deliveries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := runOnce(later, pool, e, id)
			if ok {
				ran.Add(1)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("RunEffect: %v", err)
		}
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM gate.effect_execution WHERE approval_id = $1`, string(id)).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || calls.Load() != 1 || ran.Load() != 1 {
		t.Fatalf("effect_execution rows %d, effect calls %d, ran %d; want 1, 1, 1", rows, calls.Load(), ran.Load())
	}
}
