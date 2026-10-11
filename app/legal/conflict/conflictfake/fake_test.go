package conflictfake

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/conflict"
)

func parties() []Party {
	return []Party{
		{ID: "p1", Name: "Acme Holdings Inc.", MatterID: "m1", Role: "adverse"},
		{ID: "p2", Name: "Jane Roe", MatterID: "m2", Role: "client"},
	}
}

func TestFakeCheck(t *testing.T) {
	tests := []struct {
		name     string
		terms    []string
		wantHits []string
		wantErr  error
	}{
		{"hit case insensitive", []string{"acme"}, []string{"p1"}, nil},
		{"two terms", []string{"ACME", "roe"}, []string{"p1", "p2"}, nil},
		{"no hit", []string{"Zed"}, nil, nil},
		{"no terms", nil, nil, conflict.ErrNoTerms},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(parties()...)
			run, err := f.Check(context.Background(), conflict.Request{MatterID: "m9", Terms: tt.terms, Preparer: conflict.Actor{UserID: "u1"}})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(run.Hits) != len(tt.wantHits) {
				t.Fatalf("hits = %+v, want %v", run.Hits, tt.wantHits)
			}
			for i, h := range run.Hits {
				if h.PartyID != tt.wantHits[i] {
					t.Fatalf("hit %d = %s, want %s", i, h.PartyID, tt.wantHits[i])
				}
			}
		})
	}
}

func TestFakeChecksAreAppendOnly(t *testing.T) {
	ctx := context.Background()
	f := New(parties()...)
	for i := 0; i < 2; i++ {
		if _, err := f.Check(ctx, conflict.Request{MatterID: "m9", Terms: []string{"acme"}}); err != nil {
			t.Fatal(err)
		}
	}
	runs, _ := f.Runs(ctx, "m9")
	if len(runs) != 2 || runs[0].ID == runs[1].ID {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestFakeDecide(t *testing.T) {
	lawyer := conflict.Actor{UserID: "l2", IsLawyer: true}
	tests := []struct {
		name     string
		preparer conflict.Actor
		approver conflict.Actor
		twice    bool
		want     conflict.Status
		wantErr  error
	}{
		{"other lawyer clears", conflict.Actor{UserID: "u1"}, lawyer, false, conflict.StatusCleared, nil},
		{"preparer cannot approve", conflict.Actor{UserID: "l2", IsLawyer: true}, lawyer, false, conflict.StatusOpen, conflict.ErrPreparerApproves},
		{"agent acting for approver counts as preparer", conflict.Actor{UserID: "agent", IsAgent: true, ActingFor: "l2"}, lawyer, false, conflict.StatusOpen, conflict.ErrPreparerApproves},
		{"agent cannot approve", conflict.Actor{UserID: "u1"}, conflict.Actor{UserID: "agent", IsAgent: true, IsLawyer: true}, false, conflict.StatusOpen, conflict.ErrAgentApproves},
		{"non lawyer", conflict.Actor{UserID: "u1"}, conflict.Actor{UserID: "u3"}, false, conflict.StatusOpen, conflict.ErrNotLawyer},
		{"already decided", conflict.Actor{UserID: "u1"}, lawyer, true, conflict.StatusCleared, conflict.ErrDecided},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New(parties()...)
			run, err := f.Check(ctx, conflict.Request{MatterID: "m9", Terms: []string{"acme"}, Preparer: tt.preparer})
			if err != nil {
				t.Fatal(err)
			}
			if tt.twice {
				if _, err := f.Decide(ctx, run.ID, tt.approver, true, "ok"); err != nil {
					t.Fatal(err)
				}
			}
			got, err := f.Decide(ctx, run.ID, tt.approver, true, "ok")
			if !errors.Is(err, tt.wantErr) || got.Status != tt.want {
				t.Fatalf("Decide = %s, %v; want %s, %v", got.Status, err, tt.want, tt.wantErr)
			}
		})
	}
	if _, err := New().Decide(context.Background(), "nope", lawyer, true, ""); !errors.Is(err, conflict.ErrNotFound) {
		t.Fatalf("unknown run: %v", err)
	}
}

func TestFakeWallsAndIDVerification(t *testing.T) {
	ctx := context.Background()
	f := New()
	if err := f.AddWall(ctx, conflict.Wall{MatterID: "m1", UserID: "u1", CreatedBy: "l2"}); err != nil {
		t.Fatal(err)
	}
	if err := f.RecordVerification(ctx, conflict.IDVerification{ClientID: "c1", Method: "in_person", VerifiedBy: "l2"}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		user   string
		matter string
		walled bool
		client string
		idErr  error
	}{
		{"walled user, verified client", "u1", "m1", true, "c1", nil},
		{"other user, unverified client", "u2", "m1", false, "c2", conflict.ErrIDNotVerified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Walled(ctx, tt.user, tt.matter)
			if err != nil || got != tt.walled {
				t.Fatalf("Walled = %v, %v", got, err)
			}
			if err := f.RequireVerified(ctx, tt.client); !errors.Is(err, tt.idErr) {
				t.Fatalf("RequireVerified = %v, want %v", err, tt.idErr)
			}
		})
	}
}
