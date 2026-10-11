package timekeepingfake

import (
	"context"
	"errors"
	"testing"
	"time"

	tk "github.com/dagi377/counsel/app/legal/timekeeping"
)

var (
	owner = tk.Actor{UserID: "u1"}
	other = tk.Actor{UserID: "u2"}
	agent = tk.Actor{UserID: "u1", Agent: true}
)

func entry(user, activity string) tk.TimeEntry {
	return tk.TimeEntry{MatterID: "m1", UserID: user, Minutes: 30, ActivityCode: activity, Billable: true}
}

func TestTransition(t *testing.T) {
	tests := []struct {
		name  string
		start tk.State
		path  []tk.State
		by    tk.Actor
		err   error
	}{
		{name: "owner accepts suggestion", start: tk.StateSuggested, path: []tk.State{tk.StateAccepted}, by: owner},
		{name: "agent never accepts", start: tk.StateSuggested, path: []tk.State{tk.StateAccepted}, by: agent, err: tk.ErrAgentForbidden},
		{name: "other user cannot accept", start: tk.StateSuggested, path: []tk.State{tk.StateAccepted}, by: other, err: tk.ErrNotOwner},
		{name: "suggested cannot bill", start: tk.StateSuggested, path: []tk.State{tk.StateBilled}, by: owner, err: tk.ErrBadTransition},
		{name: "self approval refused", start: tk.StateAccepted, path: []tk.State{tk.StateSubmitted, tk.StateApproved}, by: owner, err: tk.ErrSelfApproval},
		{name: "supervisor approves", start: tk.StateAccepted, path: []tk.State{tk.StateSubmitted}, by: owner},
		{name: "agent drafts bill", start: tk.StateAccepted, path: []tk.State{tk.StateApproved}, by: owner},
		{name: "agent cannot write off", start: tk.StateAccepted, path: []tk.State{tk.StateApproved, tk.StateWrittenOff}, by: agent, err: tk.ErrAgentForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New(nil, nil)
			var id tk.EntryID
			var err error
			if tt.start == tk.StateSuggested {
				id, err = f.Suggest(ctx, nil, entry("u1", "a"), agent)
			} else {
				id, err = f.Record(ctx, nil, entry("u1", "a"), owner)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range tt.path {
				if err = f.Transition(ctx, nil, id, s, tt.by); err != nil {
					break
				}
			}
			if !errors.Is(err, tt.err) {
				t.Fatalf("Transition err = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestEditLockedOnBill(t *testing.T) {
	tests := []struct {
		name  string
		path  []tk.State
		by    tk.Actor
		err   error
		wantM int
	}{
		{name: "accepted editable", by: owner, wantM: 45},
		{name: "agent cannot edit", by: agent, err: tk.ErrAgentForbidden, wantM: 30},
		{name: "on draft bill locked", path: []tk.State{tk.StateApproved, tk.StateOnDraftBill}, by: owner, err: tk.ErrBilledLocked, wantM: 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New(nil, nil)
			id, _ := f.Record(ctx, nil, entry("u1", "a"), owner)
			for _, s := range tt.path {
				if err := f.Transition(ctx, nil, id, s, owner); err != nil {
					t.Fatal(err)
				}
			}
			err := f.Edit(ctx, nil, tk.TimeEntry{ID: id, Minutes: 45}, tt.by)
			if !errors.Is(err, tt.err) {
				t.Fatalf("Edit err = %v, want %v", err, tt.err)
			}
			if got, _ := f.Get(ctx, id); got.Minutes != tt.wantM {
				t.Fatalf("minutes = %d, want %d", got.Minutes, tt.wantM)
			}
		})
	}
}

func TestTimers(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	f := New(nil, nil)
	f.Now = func() time.Time { return now }
	id, err := f.Start(ctx, nil, "u1", "m1")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		run  func() (int, error)
		want int
		err  error
	}{
		{name: "second running timer refused", run: func() (int, error) { _, err := f.Start(ctx, nil, "u1", "m2"); return 0, err }, err: tk.ErrTimerRunning},
		{name: "other user may start", run: func() (int, error) { _, err := f.Start(ctx, nil, "u2", "m1"); return 0, err }},
		{name: "stop yields raw minutes", run: func() (int, error) { now = now.Add(37*time.Minute + 20*time.Second); return f.Stop(ctx, nil, id) }, want: 37},
		{name: "stop twice", run: func() (int, error) { return f.Stop(ctx, nil, id) }, err: tk.ErrTimerStopped},
		{name: "unknown timer", run: func() (int, error) { return f.Stop(ctx, nil, "nope") }, err: tk.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.run()
			if !errors.Is(err, tt.err) || got != tt.want {
				t.Fatalf("got %d, %v; want %d, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

func TestWriteDownKeepsMinutes(t *testing.T) {
	tests := []struct {
		name string
		w    tk.WriteDown
		err  error
	}{
		{name: "pre-bill write-down", w: tk.WriteDown{OriginalCents: 15000, AdjustedCents: 10000, Kind: "write_down", Stage: "pre_bill", ProposedBy: other}},
		{name: "above original refused", w: tk.WriteDown{OriginalCents: 100, AdjustedCents: 200, Kind: "write_down", ProposedBy: other}, err: tk.ErrInvalid},
		{name: "write-up allowed", w: tk.WriteDown{OriginalCents: 100, AdjustedCents: 200, Kind: "write_up", ProposedBy: other}},
		{name: "agent refused", w: tk.WriteDown{OriginalCents: 100, AdjustedCents: 50, Kind: "write_down", ProposedBy: agent}, err: tk.ErrAgentForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New(nil, nil)
			id, _ := f.Record(ctx, nil, entry("u1", "a"), owner)
			tt.w.EntryID = id
			if err := f.WriteDown(ctx, nil, tt.w); !errors.Is(err, tt.err) {
				t.Fatalf("WriteDown err = %v, want %v", err, tt.err)
			}
			if got, _ := f.Get(ctx, id); got.Minutes != 30 {
				t.Fatalf("minutes changed to %d", got.Minutes)
			}
		})
	}
}

func TestParalegalScope(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		activity string
		err      error
	}{
		{name: "in scope", user: "p1", activity: "small_claims"},
		{name: "out of scope blocked from billing", user: "p1", activity: "family_law", err: tk.ErrOutOfScope},
		{name: "not a paralegal", user: "u1", activity: "family_law"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New([]string{"p1"}, []string{"small_claims"})
			if err := f.CheckScope(ctx, tt.user, tt.activity); !errors.Is(err, tt.err) {
				t.Fatalf("CheckScope err = %v, want %v", err, tt.err)
			}
			id, err := f.Record(ctx, nil, entry(tt.user, tt.activity), tk.Actor{UserID: tt.user})
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Transition(ctx, nil, id, tk.StateApproved, owner); err != nil {
				t.Fatal(err)
			}
			if err := f.Transition(ctx, nil, id, tk.StateOnDraftBill, owner); !errors.Is(err, tt.err) {
				t.Fatalf("bill err = %v, want %v", err, tt.err)
			}
		})
	}
}
