package deadline_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/registry"
)

var (
	person = deadline.Principal{Kind: registry.PrincipalUser, UserID: "u-1"}
	agent  = deadline.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "u-1"}
)

func proposal(t *testing.T, p deadline.Pack, rule string, trigger deadline.Date) deadline.Proposed {
	t.Helper()
	due, err := deadline.Due(p, rule, trigger)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	return deadline.Proposed{PackID: p.ID, PackVersion: p.Version, RuleID: rule, Trigger: trigger, Due: due,
		Subject: deadline.Subject{Kind: "ticket", ID: "t-1"}}
}

func TestConfirmNeedsPerson(t *testing.T) {
	ctx := context.Background()
	p := helpdesk(t)
	tests := []struct {
		name      string
		by        deadline.Principal
		twice     bool
		wantErr   error
		wantState deadline.State
	}{
		{"person confirms", person, false, nil, deadline.StateConfirmed},
		{"agent refused", agent, false, deadline.ErrAgentCannotConfirm, deadline.StateProposed},
		{"guest refused", deadline.Principal{Kind: registry.PrincipalGuest, UserID: "g-1"}, false, deadline.ErrAgentCannotConfirm, deadline.StateProposed},
		{"anonymous user refused", deadline.Principal{Kind: registry.PrincipalUser}, false, deadline.ErrAgentCannotConfirm, deadline.StateProposed},
		{"unknown kind refused", deadline.Principal{UserID: "u-1"}, false, deadline.ErrAgentCannotConfirm, deadline.StateProposed},
		{"second confirm refused", person, true, deadline.ErrAlreadyConfirmed, deadline.StateConfirmed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := deadline.NewBook()
			id, err := b.Save(ctx, proposal(t, p, "auto_close", date(2026, time.March, 2)))
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			if d, _ := b.Get(ctx, id); d.State != deadline.StateProposed {
				t.Fatalf("new deadline state = %s, want proposed", d.State)
			}
			if tc.twice {
				if err := b.Confirm(ctx, id, tc.by); err != nil {
					t.Fatalf("first Confirm: %v", err)
				}
			}
			err = b.Confirm(ctx, id, tc.by)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Confirm err = %v, want %v", err, tc.wantErr)
			}
			d, err := b.Get(ctx, id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if d.State != tc.wantState {
				t.Fatalf("state = %s, want %s", d.State, tc.wantState)
			}
			wantLog := 0
			if tc.wantState == deadline.StateConfirmed {
				wantLog = 1
			}
			if got := b.Confirmations(ctx, id); len(got) != wantLog {
				t.Fatalf("confirmation log = %v, want %d entries", got, wantLog)
			}
		})
	}
}

func TestConfirmUnknownDeadline(t *testing.T) {
	ctx := context.Background()
	b := deadline.NewBook()
	if err := b.Confirm(ctx, "missing", person); !errors.Is(err, deadline.ErrNotFound) {
		t.Fatalf("Confirm err = %v, want ErrNotFound", err)
	}
	if _, err := b.Get(ctx, "missing"); !errors.Is(err, deadline.ErrNotFound) {
		t.Fatalf("Get err = %v, want ErrNotFound", err)
	}
}

func TestConfirmationLogIsAppendOnly(t *testing.T) {
	ctx := context.Background()
	p := helpdesk(t)
	b := deadline.NewBook()
	id, err := b.Save(ctx, proposal(t, p, "auto_close", date(2026, time.March, 2)))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Confirm(ctx, id, person); err != nil {
		t.Fatal(err)
	}
	got := b.Confirmations(ctx, id)
	want := []deadline.Confirmation{{DeadlineID: id, UserID: "u-1", Due: date(2026, time.March, 16)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("log = %v, want %v", got, want)
	}
	got[0].UserID = "tampered"
	if again := b.Confirmations(ctx, id); !reflect.DeepEqual(again, want) {
		t.Fatalf("log changed through returned copy: %v", again)
	}
	// A new pack version does not touch the confirmed deadline or its log entry.
	v2 := p
	v2.Version = "2"
	v2.Rules = []deadline.Rule{{ID: "auto_close", Mode: deadline.Calendar, Days: 21, Roll: deadline.RollNone}}
	if _, err := b.Repack(ctx, v2); err != nil {
		t.Fatal(err)
	}
	if again := b.Confirmations(ctx, id); !reflect.DeepEqual(again, want) {
		t.Fatalf("log changed by repack: %v", again)
	}
}

func TestRepackRecomputesUnconfirmedOnly(t *testing.T) {
	ctx := context.Background()
	v1 := helpdesk(t)
	trigger := date(2026, time.March, 2)
	v2 := v1
	v2.Version = "2"
	v2.Rules = []deadline.Rule{
		{ID: "first_response", Mode: deadline.Business, Days: 2, Roll: deadline.RollForward},
		{ID: "auto_close", Mode: deadline.Calendar, Days: 21, Roll: deadline.RollNone},
	}
	other := deadline.Pack{ID: "other", Version: "1", Rules: []deadline.Rule{{ID: "auto_close", Mode: deadline.Calendar, Days: 3}}}

	tests := []struct {
		name        string
		pack        deadline.Pack
		rule        string
		confirm     bool
		repack      deadline.Pack
		wantChanged bool
		wantDue     deadline.Date
		wantVersion string
	}{
		{"unconfirmed calendar recomputed", v1, "auto_close", false, v2, true, date(2026, time.March, 23), "2"},
		{"unconfirmed business recomputed", v1, "first_response", false, v2, true, date(2026, time.March, 4), "2"},
		{"confirmed kept", v1, "auto_close", true, v2, false, date(2026, time.March, 16), "1"},
		{"same version is a no-op", v1, "auto_close", false, v1, false, date(2026, time.March, 16), "1"},
		{"other pack untouched", other, "auto_close", false, v2, false, date(2026, time.March, 5), "1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := deadline.NewBook()
			id, err := b.Save(ctx, proposal(t, tc.pack, tc.rule, trigger))
			if err != nil {
				t.Fatal(err)
			}
			if tc.confirm {
				if err := b.Confirm(ctx, id, person); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := b.Repack(ctx, tc.repack)
			if err != nil {
				t.Fatalf("Repack: %v", err)
			}
			if got := len(changed) == 1 && changed[0] == id; got != tc.wantChanged {
				t.Fatalf("changed = %v, want changed %t", changed, tc.wantChanged)
			}
			d, err := b.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if d.Proposed.Due != tc.wantDue || d.Proposed.PackVersion != tc.wantVersion {
				t.Fatalf("due %s v%s, want %s v%s", d.Proposed.Due, d.Proposed.PackVersion, tc.wantDue, tc.wantVersion)
			}
			wantState := deadline.StateProposed
			if tc.confirm {
				wantState = deadline.StateConfirmed
			}
			if d.State != wantState {
				t.Fatalf("state = %s, want %s", d.State, wantState)
			}
		})
	}
}

func TestRepackFailsWithoutPartialChange(t *testing.T) {
	ctx := context.Background()
	v1 := helpdesk(t)
	b := deadline.NewBook()
	trigger := date(2026, time.March, 2)
	keep, err := b.Save(ctx, proposal(t, v1, "auto_close", trigger))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Save(ctx, proposal(t, v1, "first_response", trigger)); err != nil {
		t.Fatal(err)
	}
	// v2 drops first_response, so that deadline cannot be re-computed.
	v2 := v1
	v2.Version = "2"
	v2.Rules = []deadline.Rule{{ID: "auto_close", Mode: deadline.Calendar, Days: 21}}
	tests := []struct {
		name string
		pack deadline.Pack
		want error
	}{
		{"rule removed", v2, deadline.ErrUnknownRule},
		{"no version", deadline.Pack{ID: v1.ID}, deadline.ErrInvalidPack},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := b.Repack(ctx, tc.pack); !errors.Is(err, tc.want) {
				t.Fatalf("Repack err = %v, want %v", err, tc.want)
			}
			d, err := b.Get(ctx, keep)
			if err != nil {
				t.Fatal(err)
			}
			if d.Proposed.PackVersion != "1" || d.Proposed.Due != date(2026, time.March, 16) {
				t.Fatalf("deadline changed on failed repack: %+v", d.Proposed)
			}
		})
	}
}
