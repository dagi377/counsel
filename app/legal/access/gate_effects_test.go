package access

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatestest"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

// rule: ARC-I-012
// rule: ARC-I-016
func TestOffboardingEffect(t *testing.T) {
	const (
		later = `{"user_id":"u-leaver","handover":["u-next"],"cutoff_at":"2026-10-20T17:00:00-04:00"}`
		now   = `{"user_id":"u-leaver","handover":[{"user_id":"u-next"}],"cutoff_at":"2026-10-11T12:00:00-04:00"}`
	)
	leaver := registry.Principal{Kind: registry.PrincipalUser, UserID: "u-leaver"}
	L, P, A := gatestest.Lawyer, gatestest.Prep, gatestest.Agent
	tests := []struct {
		name      string
		g         registry.Approved
		wantApply bool
		wantErr   error
	}{
		{"future cut-off is scheduled, not applied", gatestest.Gate(KindOffboarding, later, A, L), false, nil},
		{"past cut-off applies now", gatestest.Gate(KindOffboarding, now, A, L), true, nil},
		{"no hand-over user fails closed", gatestest.Gate(KindOffboarding, `{"user_id":"u-leaver","handover":[{}],"cutoff_at":"2026-10-20T17:00:00Z"}`, A, L), false, gates.ErrNotApprovable},
		{"the leaver never approves", gatestest.Gate(KindOffboarding, later, A, leaver), false, gates.ErrPreparerApproves},
		{"agent never approves", gatestest.Gate(KindOffboarding, later, P, A), false, gates.ErrAgentApproves},
		{"preparer never approves", gatestest.Gate(KindOffboarding, later, A, P), false, gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &gatestest.Tx{}
			applied := false
			err := scheduleOffboarding(context.Background(), tx, tt.g, gatestest.Now, func(context.Context) error { applied = true; return nil })
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if applied != tt.wantApply {
				t.Fatalf("applied = %v, want %v", applied, tt.wantApply)
			}
			if (tt.wantErr == nil) != tx.Ran("ON CONFLICT (gate_id) DO NOTHING") {
				t.Fatalf("ran %q", tx.SQLs)
			}
		})
	}
}

// rule: ARC-I-012
func TestApplyDueOffboardings(t *testing.T) {
	tests := []struct {
		name        string
		scans       [][]any
		rows        []int64
		wantHanded  int
		wantPublish int
	}{
		{"nothing due", nil, nil, 0, 0},
		{"due offboarding hands over, offboards and publishes", [][]any{{"g-1", "u-leaver", "u-next", "u-owner"}}, nil, 1, 1},
		{"already offboarded publishes nothing", [][]any{{"g-1", "u-leaver", "u-next", "u-owner"}}, []int64{0}, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &gatestest.Tx{Scans: tt.scans, Rows: tt.rows}
			handed, published := 0, 0
			hand := func(_ context.Context, _ gates.Execer, from, to string, _ time.Time) error {
				if from != "u-leaver" || to != "u-next" {
					t.Fatalf("handover %s -> %s", from, to)
				}
				handed++
				return nil
			}
			err := applyDue(context.Background(), tx, gatestest.Now(), hand, func(_ context.Context, b identity.Deactivated) error {
				if b.UserID != "u-leaver" || b.GateID != "g-1" {
					t.Fatalf("published %+v", b)
				}
				published++
				return nil
			})
			if err != nil || handed != tt.wantHanded || published != tt.wantPublish {
				t.Fatalf("err %v, handed %d, published %d; want %d, %d", err, handed, published, tt.wantHanded, tt.wantPublish)
			}
		})
	}
}
