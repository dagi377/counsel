package timekeeping_test

import (
	"context"
	"errors"
	"testing"
	"time"

	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/timekeeping/timekeepingfake"
)

// TestEntriesContract runs the suggestion half of the Entries contract against the fake: a
// suggestion stays suggested, an agent never accepts it, only its owner does, and an unknown
// entry is not found.
//
// rule: ARC-I-035
func TestEntriesContract(t *testing.T) {
	ctx := context.Background()
	f := timekeepingfake.New(nil, nil)
	owner := tk.Actor{UserID: "u1"}
	agentFor := tk.Actor{UserID: "u1", Agent: true}
	e := tk.TimeEntry{MatterID: "m1", UserID: "u1", WorkDate: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC),
		Minutes: 42, Narrative: "review", Billable: true}
	id, err := f.Suggest(ctx, nil, e, agentFor)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		run       func() error
		want      error
		wantState tk.State
	}{
		{"suggested, not accepted", func() error { return nil }, nil, tk.StateSuggested},
		{"agent cannot accept", func() error { return f.Transition(ctx, nil, id, tk.StateAccepted, agentFor) }, tk.ErrAgentForbidden, tk.StateSuggested},
		{"another user cannot accept", func() error {
			return f.Transition(ctx, nil, id, tk.StateAccepted, tk.Actor{UserID: "u2"})
		}, tk.ErrNotOwner, tk.StateSuggested},
		{"owner accepts", func() error { return f.Transition(ctx, nil, id, tk.StateAccepted, owner) }, nil, tk.StateAccepted},
		{"agent cannot record", func() error { _, err := f.Record(ctx, nil, e, agentFor); return err }, tk.ErrAgentForbidden, tk.StateAccepted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			got, err := f.Get(ctx, id)
			if err != nil || got.State != tc.wantState {
				t.Fatalf("state = %s, %v; want %s", got.State, err, tc.wantState)
			}
		})
	}
	if _, err := f.Get(ctx, "nope"); !errors.Is(err, tk.ErrNotFound) {
		t.Fatalf("unknown entry: %v", err)
	}
}

// TestPGEntriesRefuseBeforeWriting: PGEntries checks who acts and what is recorded before it
// touches the database, so these calls fail the same way with no pool.
//
// rule: ARC-I-035
func TestPGEntriesRefuseBeforeWriting(t *testing.T) {
	ctx := context.Background()
	p := tk.PGEntries{}
	day := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	e := tk.TimeEntry{MatterID: "m1", UserID: "u1", WorkDate: day, Minutes: 30, Narrative: "call"}
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{"agent records", func() error { _, err := p.Record(ctx, nil, e, tk.Actor{UserID: "u1", Agent: true}); return err }, tk.ErrAgentForbidden},
		{"record for someone else", func() error { _, err := p.Record(ctx, nil, e, tk.Actor{UserID: "u2"}); return err }, tk.ErrNotOwner},
		{"suggest for someone else", func() error {
			_, err := p.Suggest(ctx, nil, e, tk.Actor{UserID: "u2", Agent: true})
			return err
		}, tk.ErrNotOwner},
		{"no minutes", func() error {
			bad := e
			bad.Minutes = 0
			_, err := p.Suggest(ctx, nil, bad, tk.Actor{UserID: "u1", Agent: true})
			return err
		}, tk.ErrInvalid},
		{"over a day", func() error {
			bad := e
			bad.Minutes = 1441
			_, err := p.Suggest(ctx, nil, bad, tk.Actor{UserID: "u1", Agent: true})
			return err
		}, tk.ErrInvalid},
		{"agent edits", func() error { return p.Edit(ctx, nil, e, tk.Actor{UserID: "u1", Agent: true}) }, tk.ErrAgentForbidden},
		{"get without a database", func() error { _, err := p.Get(ctx, "x"); return err }, tk.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
