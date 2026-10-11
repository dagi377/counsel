package matter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/conflict/conflictfake"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

var (
	clerk  = conflict.Actor{UserID: "u-clerk"}
	lawyer = conflict.Actor{UserID: "u-law", IsLawyer: true}
)

// runs records one conflict run per decision for matter m; "" leaves the run open.
func runs(t *testing.T, f *conflictfake.Fake, m string, decisions ...string) {
	t.Helper()
	ctx := context.Background()
	for _, d := range decisions {
		r, err := f.Check(ctx, conflict.Request{MatterID: m, Terms: []string{"acme"}, Preparer: clerk})
		if err != nil {
			t.Fatal(err)
		}
		if d == "" {
			continue
		}
		if _, err := f.Decide(ctx, r.ID, lawyer, d == "clear", "reviewed"); err != nil {
			t.Fatal(err)
		}
	}
}

func lifecycle(t *testing.T, f *conflictfake.Fake) *matter.Lifecycle {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	// 02:30 UTC on 2 March is still 1 March in Toronto.
	c := clockfake.New(time.Date(2026, 3, 2, 2, 30, 0, 0, time.UTC), loc)
	return &matter.Lifecycle{Clock: c, Conflicts: f, Walls: f}
}

func TestMatterOpensOnlyAfterG2Clearance(t *testing.T) {
	tests := []struct {
		name      string
		decisions []string
		wantErr   error
	}{
		{"no conflict run", nil, matter.ErrNotCleared},
		{"run awaiting G2", []string{""}, matter.ErrNotCleared},
		{"G2 rejected", []string{"reject"}, matter.ErrNotCleared},
		{"G2 cleared", []string{"clear"}, nil},
		{"cleared then re-check open", []string{"clear", ""}, matter.ErrNotCleared},
		{"rejected then cleared", []string{"reject", "clear"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := conflictfake.New(conflictfake.Party{ID: "p1", Name: "Acme Ltd", MatterID: "m-old", Role: "adverse"})
			runs(t, f, "m1", tc.decisions...)
			got, err := lifecycle(t, f).Transition(context.Background(), matter.Matter{ID: "m1", Status: matter.Pending}, "u-law", matter.Open)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if got.Status != matter.Open {
				t.Fatalf("status = %s, want open", got.Status)
			}
			if y, m, d := got.OpenDate.Date(); y != 2026 || m != time.March || d != 1 || got.OpenDate.Location().String() != "America/Toronto" {
				t.Fatalf("open date = %v, want 2026-03-01 America/Toronto", got.OpenDate)
			}
		})
	}
}

func TestMatterTransitions(t *testing.T) {
	tests := []struct {
		from, to matter.Status
		ok       bool
	}{
		{matter.Open, matter.Closing, true},
		{matter.Closing, matter.Closed, true},
		{matter.Closing, matter.Open, true},
		{matter.Closed, matter.Archived, true},
		{matter.Pending, matter.Closed, false},
		{matter.Open, matter.Closed, false},
		{matter.Archived, matter.Open, false},
		{matter.Closed, matter.Open, false},
		{matter.Open, matter.Open, false},
	}
	for _, tc := range tests {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			if got := matter.CanTransition(tc.from, tc.to); got != tc.ok {
				t.Fatalf("CanTransition = %v, want %v", got, tc.ok)
			}
			f := conflictfake.New()
			got, err := lifecycle(t, f).Transition(context.Background(), matter.Matter{ID: "m1", Status: tc.from}, "u-law", tc.to)
			if tc.ok && (err != nil || got.Status != tc.to) {
				t.Fatalf("got %s, %v; want %s", got.Status, err, tc.to)
			}
			if !tc.ok && !errors.Is(err, matter.ErrBadTransition) {
				t.Fatalf("err = %v, want ErrBadTransition", err)
			}
		})
	}
}

// rule: ARC-I-041
func TestARC_I_041_ClearanceReadThroughConflictAPI(t *testing.T) {
	tests := []struct {
		name    string
		matter  string
		wantErr error
	}{
		{"clearance on this matter", "m1", nil},
		{"clearance on another matter does not count", "m2", matter.ErrNotCleared},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := conflictfake.New()
			runs(t, f, "m1", "clear")
			_, err := lifecycle(t, f).Transition(context.Background(), matter.Matter{ID: matter.ID(tc.matter), Status: matter.Pending}, "u-law", matter.Open)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// rule: ARC-I-042
func TestARC_I_042_WalledUserGetsNotFound(t *testing.T) {
	tests := []struct {
		name    string
		actor   string
		wantErr error
	}{
		{"walled user", "u-walled", matter.ErrNotFound},
		{"other user", "u-law", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := conflictfake.New()
			if err := f.AddWall(context.Background(), conflict.Wall{MatterID: "m1", UserID: "u-walled", CreatedBy: "u-law", Reason: "lateral hire"}); err != nil {
				t.Fatal(err)
			}
			_, err := lifecycle(t, f).Transition(context.Background(), matter.Matter{ID: "m1", Status: matter.Open}, tc.actor, matter.Closing)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
