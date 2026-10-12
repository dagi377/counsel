//go:build integration

package gate_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/gate"
)

// The effect context carries the approving gate.gate_decision id, so a column that
// references gate.gate_decision (an app's confirmation log) stores a row that exists: the
// id is a decision of this gate, not the gate (approval_request) id.
func TestEffectContextCarriesDecisionID(t *testing.T) {
	pool := effectDB(t)
	clk, err := clock.New("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	ctx := clock.WithClock(context.Background(), clk)
	users := addUsers(t, ctx, pool, "preparer@example.com", "approver@example.com")
	preparer, approver := users[0], users[1]

	var seen, gateID string
	var isDecision bool
	e := gate.NewPG()
	k := gate.GateKind{
		Slug: "desk.refund", Approver: "desk.approve", Risk: gate.RiskMedium, MinApprovers: 1,
		TTL: 24 * time.Hour, PayloadSchema: []byte(`{"type":"object"}`),
		Effect: func(ctx context.Context, tx pgx.Tx, g gate.Approved) error {
			d, ok := gate.DecisionOf(ctx)
			if !ok {
				t.Error("effect context has no decision id")
				return nil
			}
			seen, gateID = d, g.GateID
			return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM gate.gate_decision
 WHERE id = $1::uuid AND approval_id = $2::uuid AND decision = 'approve')`, d, g.GateID).Scan(&isDecision)
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
	subject := gate.Subject{Kind: "desk.refund_request", ID: "0190d0f4-0000-7000-8000-000000000002"}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		id, err := e.Open(ctx, tx, k.Slug, subject, payload, gate.Principal{Kind: gate.PrincipalUser, UserID: preparer})
		if err != nil {
			return err
		}
		return e.Decide(ctx, tx, id, 1, gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: hash, MFAVerifiedAt: time.Now()},
			gate.Principal{Kind: gate.PrincipalUser, UserID: approver})
	})
	if err != nil {
		t.Fatalf("open and approve: %v", err)
	}
	if seen == "" || seen == gateID {
		t.Fatalf("decision id = %q, gate id = %q; want a decision id distinct from the gate id", seen, gateID)
	}
	if !isDecision {
		t.Fatalf("decision id %s is not an approving gate.gate_decision row of gate %s", seen, gateID)
	}
}
