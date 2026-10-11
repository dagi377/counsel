package conflict

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
)

var clearT0 = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

type capsMap map[string][]string

func (m capsMap) Capabilities(_ context.Context, userID string) ([]string, error) {
	return m[userID], nil
}

type hitSearch struct{}

func (hitSearch) Search(_ context.Context, term string) ([]Hit, error) {
	return []Hit{{Term: term, PartyID: "p-1", MatterID: "M-0", Role: "adverse"}}, nil
}

// clearance returns a Clearance over a fresh Checks with one open run prepared by prep, and
// a clock that reads *at.
func clearance(t *testing.T, cooling time.Duration, prep Actor, at *time.Time) (*Clearance, *Checks, Run) {
	t.Helper()
	g, err := gatesfake.New(gates.Kind{Code: GateClearance, SpecGate: "G2", Approver: "conflicts.clear", CoolingOff: cooling})
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return *at }
	checks := NewChecks(hitSearch{}, now)
	run, err := checks.Check(context.Background(), Request{MatterID: "M-1", Terms: []string{"Acme"}, Preparer: prep})
	if err != nil {
		t.Fatal(err)
	}
	caps := capsMap{"lawyer-b": {"conflicts.clear"}, "lawyer-a": {"conflicts.clear"}, "clerk": {"conflicts.clear"}}
	return NewClearance(checks, g, caps, now), checks, run
}

func TestClearanceDecide(t *testing.T) {
	lawyerA := Actor{UserID: "lawyer-a", IsLawyer: true}
	lawyerB := Actor{UserID: "lawyer-b", IsLawyer: true}
	agentForA := Actor{UserID: "agent-1", IsAgent: true, ActingFor: "lawyer-a"}
	tests := []struct {
		name     string
		prep     Actor
		approver Actor
		approve  bool
		cooling  time.Duration
		want     Status
		wantErr  []error
	}{
		{"another lawyer clears", lawyerA, lawyerB, true, 0, StatusCleared, nil},
		{"another lawyer rejects", lawyerA, lawyerB, false, 0, StatusRejected, nil},
		{"preparer cannot clear", lawyerA, lawyerA, true, 0, StatusOpen, []error{ErrPreparerApproves, gates.ErrPreparerApproves}},
		{"user an agent prepared for cannot clear", agentForA, lawyerA, true, 0, StatusOpen, []error{ErrPreparerApproves}},
		{"agent cannot clear", lawyerA, Actor{UserID: "agent-2", IsAgent: true, ActingFor: "lawyer-b"}, true, 0, StatusOpen, []error{ErrAgentApproves, gates.ErrAgentApproves}},
		{"non-lawyer cannot clear", lawyerA, Actor{UserID: "clerk"}, true, 0, StatusOpen, []error{ErrNotLawyer}},
		{"lawyer without the G2 capability cannot clear", lawyerA, Actor{UserID: "lawyer-c", IsLawyer: true}, true, 0, StatusOpen, []error{gates.ErrMissingCapability}},
		{"G2 cooling-off applies", lawyerA, lawyerB, true, time.Hour, StatusOpen, []error{gates.ErrCoolingOff}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at := clearT0
			c, checks, run := clearance(t, tc.cooling, tc.prep, &at)
			got, err := c.Decide(context.Background(), run.ID, tc.approver, tc.approve, "reviewed")
			for _, w := range tc.wantErr {
				if !errors.Is(err, w) {
					t.Fatalf("err = %v, want %v", err, w)
				}
			}
			if tc.wantErr == nil && err != nil {
				t.Fatalf("err = %v", err)
			}
			if got.Status != tc.want {
				t.Fatalf("status = %s, want %s", got.Status, tc.want)
			}
			runs, err := checks.Runs(context.Background(), "M-1")
			if err != nil {
				t.Fatal(err)
			}
			if len(runs) != 1 || runs[0].Status != tc.want {
				t.Fatalf("stored runs = %+v, want one run %s", runs, tc.want)
			}
		})
	}
}

// Check run rows stay append-only: a decision does not change what the run recorded, a
// decided run cannot be decided again, and a re-check adds a new run.
func TestClearanceAppendOnly(t *testing.T) {
	ctx := context.Background()
	lawyerB := Actor{UserID: "lawyer-b", IsLawyer: true}
	tests := []struct {
		name string
		step func(t *testing.T, c *Clearance, checks *Checks, run Run) error
		want error
		runs int
	}{
		{"second decision refused", func(t *testing.T, c *Clearance, _ *Checks, run Run) error {
			_, err := c.Decide(ctx, run.ID, lawyerB, false, "changed mind")
			return err
		}, ErrDecided, 1},
		{"re-check appends a new open run", func(t *testing.T, _ *Clearance, checks *Checks, _ Run) error {
			r, err := checks.Check(ctx, Request{MatterID: "M-1", Terms: []string{"Acme Ltd"}, Preparer: Actor{UserID: "lawyer-a"}})
			if err == nil && r.Status != StatusOpen {
				t.Fatalf("re-check status = %s", r.Status)
			}
			return err
		}, nil, 2},
		{"unknown run refused", func(t *testing.T, c *Clearance, _ *Checks, _ Run) error {
			_, err := c.Decide(ctx, "run-404", lawyerB, true, "")
			return err
		}, ErrNotFound, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at := clearT0
			c, checks, run := clearance(t, 0, Actor{UserID: "lawyer-a", IsLawyer: true}, &at)
			at = clearT0.Add(time.Minute)
			cleared, err := c.Decide(ctx, run.ID, lawyerB, true, "no conflict")
			if err != nil {
				t.Fatal(err)
			}
			cleared.Hits[0].Role = "edited by caller"
			if err := tc.step(t, c, checks, run); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			runs, err := checks.Runs(ctx, "M-1")
			if err != nil {
				t.Fatal(err)
			}
			if len(runs) != tc.runs {
				t.Fatalf("runs = %d, want %d", len(runs), tc.runs)
			}
			first := runs[0]
			if !reflect.DeepEqual(first.Terms, run.Terms) || !reflect.DeepEqual(first.Hits, run.Hits) ||
				first.PreparedBy != run.PreparedBy || !first.RanAt.Equal(run.RanAt) {
				t.Fatalf("run rows changed: %+v, was %+v", first, run)
			}
			if first.Status != StatusCleared || first.DecidedBy != "lawyer-b" || !first.DecidedAt.Equal(at) || first.Reason != "no conflict" {
				t.Fatalf("decision = %+v", first)
			}
		})
	}
}
