package gatefake

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
)

func TestOnReject(t *testing.T) {
	tests := []struct {
		name       string
		hookErr    error
		verdict    gate.Verdict
		wantCalls  int
		wantErr    bool
		wantStatus gate.Status
	}{
		{name: "reject runs hook", verdict: gate.VerdictReject, wantCalls: 1, wantStatus: gate.StatusRejected},
		{name: "hook error keeps gate pending", hookErr: errors.New("boom"), verdict: gate.VerdictReject, wantCalls: 1, wantErr: true, wantStatus: gate.StatusPending},
		{name: "changes requested runs no hook", verdict: gate.VerdictChangesRequested, wantStatus: gate.StatusChangesRequested},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := kind("x.pay", 1, 0)
			var got []gate.Rejected
			k.OnReject = func(_ context.Context, _ pgx.Tx, g gate.Rejected) error {
				got = append(got, g)
				return tt.hookErr
			}
			e := newEnv(t, k)
			ctx := context.Background()
			id, err := e.f.Open(ctx, nil, "x.pay", subject, payload, alice)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			g, _ := e.f.Get(ctx, id)
			d := gate.Decision{Verdict: tt.verdict, Reason: "no", PayloadHash: g.PayloadHash, MFAVerifiedAt: t0}
			err = e.f.Decide(ctx, nil, id, g.Version, d, bob)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Decide err = %v, wantErr %v", err, tt.wantErr)
			}
			if len(got) != tt.wantCalls {
				t.Fatalf("hook calls = %d, want %d", len(got), tt.wantCalls)
			}
			if tt.wantCalls > 0 && (got[0].Subject != subject || got[0].RejectedBy.UserID != "bob" || got[0].Reason != "no") {
				t.Fatalf("hook saw %+v", got[0])
			}
			if g, _ = e.f.Get(ctx, id); g.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s", g.Status, tt.wantStatus)
			}
		})
	}
}
