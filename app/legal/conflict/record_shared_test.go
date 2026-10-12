package conflict_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

type oneHit struct{}

func (oneHit) Search(_ context.Context, term string) ([]conflict.Hit, error) {
	return []conflict.Hit{{Term: term, PartyID: "p-1", MatterID: "M-0", Role: "adverse"}}, nil
}

type noWalls struct{}

func (noWalls) AddWall(context.Context, conflict.Wall) error         { return nil }
func (noWalls) Walled(context.Context, string, string) (bool, error) { return false, nil }

type caps map[string][]string

func (m caps) Capabilities(_ context.Context, id string) ([]string, error) { return m[id], nil }

// sharedRecord is the conflict record two service instances share: Postgres in production.
type sharedRecord struct {
	runs    conflict.RunStore
	records conflict.DecisionStore
}

// instance is one process: its own Checks over the shared record.
func (s sharedRecord) instance(now func() time.Time) *conflict.Checks {
	return conflict.NewChecksStored(oneHit{}, s.runs, s.records, now)
}

// rule: ARC-I-041 (LSO RPC 3.4 conflict record)
// A G2 clearance recorded through one instance is the record matter opening reads in
// another instance and after a restart; without it, or with the consent missing, the matter
// stays pending.
func TestClearanceRecordIsShared(t *testing.T) {
	t0 := time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC)
	now := func() time.Time { return t0 }
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	prep := conflict.Actor{UserID: "lawyer-a", IsLawyer: true}
	law := conflict.Actor{UserID: "lawyer-b", IsLawyer: true}
	signed := conflict.Consent{Type: conflict.ConsentJointRetainer, Status: conflict.ConsentSigned,
		DisclosureSummary: "acts for both", DocumentID: "doc-1", SignedAt: t0}
	tests := []struct {
		name      string
		disp      conflict.Disposition
		consent   *conflict.Consent
		decide    bool
		wantClear error
		wantOpen  error
	}{
		{"cleared in A opens in B", conflict.DispositionNotAConflict, nil, true, nil, nil},
		{"clearance survives a restart", conflict.DispositionNotAConflict, nil, true, nil, nil},
		{"signed consent clears and opens", conflict.DispositionNeedsConsent, &signed, true, nil, nil},
		{"no clearance refuses opening", conflict.DispositionNotAConflict, nil, false, nil, matter.ErrNotCleared},
		{"consent note only refuses clearing and opening", conflict.DispositionNeedsConsent, nil, true,
			conflict.ErrConsentMissing, matter.ErrNotCleared},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			shared := sharedRecord{runs: conflict.NewMemRuns(), records: conflict.NewMemDecisions()}
			a := shared.instance(now)
			g, err := gatesfake.New(gates.Kind{Code: conflict.GateClearance, SpecGate: "G2", Approver: "conflicts.clear"})
			if err != nil {
				t.Fatal(err)
			}
			clear := conflict.NewClearance(a, g, caps{"lawyer-b": {"conflicts.clear"}}, now)
			run, err := a.Check(ctx, conflict.Request{MatterID: "M-1", Terms: []string{"Acme"}, Preparer: prep})
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Dispose(ctx, run.ID, 0, law, tt.disp, "reviewed"); err != nil {
				t.Fatal(err)
			}
			if tt.consent != nil {
				if err := a.RecordConsent(ctx, run.ID, 0, law, *tt.consent); err != nil {
					t.Fatal(err)
				}
			}
			if tt.decide {
				if _, err := clear.Decide(ctx, run.ID, law, true, "no conflict"); !errors.Is(err, tt.wantClear) {
					t.Fatalf("clear err = %v, want %v", err, tt.wantClear)
				}
			}
			// B is a second replica or, for a restart, the new process: either way it holds
			// nothing of A and reads only the stored record.
			b := shared.instance(now)
			l := &matter.Lifecycle{Clock: clockfake.New(t0, loc), Conflicts: b, Walls: noWalls{}}
			got, err := l.Transition(ctx, matter.Matter{ID: "M-1", Status: matter.Pending}, "lawyer-b", matter.Open)
			if !errors.Is(err, tt.wantOpen) {
				t.Fatalf("open err = %v, want %v", err, tt.wantOpen)
			}
			if tt.wantOpen == nil && got.Status != matter.Open {
				t.Fatalf("status = %s, want open", got.Status)
			}
		})
	}
}
