//go:build integration

package gate_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// poolRunner runs fn in one pool transaction, like kernel/db.DB.InTx.
type poolRunner struct{ pool *pgxpool.Pool }

func (r poolRunner) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error { return fn(ctx, tx) })
}

// The engine on Postgres: withdraw is preparer-only and closes the gate; RunDue runs each
// due effect once; Get reads the stored state.
func TestPGEngineWithdrawRunDueGet(t *testing.T) {
	pool := effectDB(t)
	clk, err := clock.New("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	ctx := clock.WithClock(context.Background(), clk)
	users := addUsers(t, ctx, pool, "preparer@example.com", "approver@example.com")
	preparer := gate.Principal{Kind: gate.PrincipalUser, UserID: users[0]}
	approver := gate.Principal{Kind: gate.PrincipalUser, UserID: users[1]}

	var calls atomic.Int32
	e := gate.NewPG()
	e.SetDB(poolRunner{pool})
	k := gate.GateKind{
		Slug: "desk.refund", Approver: "desk.approve", Risk: gate.RiskMedium, MinApprovers: 1,
		TTL: 24 * time.Hour, CoolingOff: time.Hour, PayloadSchema: []byte(`{"type":"object"}`),
		Effect: func(context.Context, pgx.Tx, gate.Approved) error { calls.Add(1); return nil },
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
	open := func(subjectID string) gate.GateID {
		var id gate.GateID
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			id, err = e.Open(ctx, tx, k.Slug, gate.Subject{Kind: "desk.refund_request", ID: subjectID}, payload, preparer)
			return err
		}); err != nil {
			t.Fatalf("open: %v", err)
		}
		return id
	}

	withdrawn := open("0190d0f4-0000-7000-8000-000000000001")
	tests := []struct {
		name string
		by   gate.Principal
		want error
	}{
		{"non-preparer refused", approver, gate.ErrNotPreparer},
		{"preparer withdraws", preparer, nil},
		{"second withdraw refused", preparer, gate.ErrNotPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return e.Withdraw(ctx, tx, withdrawn, tt.by) })
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("Withdraw = %v, want %v", err, tt.want)
			}
		})
	}
	if g, err := e.Get(ctx, withdrawn); err != nil || g.Status != gate.StatusWithdrawn {
		t.Fatalf("Get after withdraw = (%v, %v), want withdrawn", g.Status, err)
	}

	due := open("0190d0f4-0000-7000-8000-000000000002")
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return e.Decide(ctx, tx, due, 1, gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: hash, MFAVerifiedAt: time.Now()}, approver)
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	later := clock.WithClock(context.Background(), clockfake.New(time.Now().Add(2*time.Hour), clk.Location()))
	for i, want := range []int{1, 0} {
		var n int
		if err := pgx.BeginFunc(later, pool, func(tx pgx.Tx) error {
			n, err = e.RunDue(later, tx)
			return err
		}); err != nil {
			t.Fatalf("RunDue %d: %v", i, err)
		}
		if n != want {
			t.Fatalf("RunDue %d = %d, want %d", i, n, want)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("effect calls = %d, want 1", calls.Load())
	}
	g, err := e.Get(ctx, due)
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != gate.StatusApproved || !g.EffectRan || len(g.Approvers) != 1 || g.Approvers[0].UserID != approver.UserID {
		t.Fatalf("Get = %+v", g)
	}
	if _, err := e.Get(ctx, "0190d0f4-0000-7000-8000-0000000000ff"); !errors.Is(err, gate.ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}
}
