package deadlinefake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/deadline"
)

func day(m time.Month, d int) deadline.Date { return deadline.Date{Year: 2026, Month: m, Day: d} }

// helpdeskPack is a non-domain pack: a support ticket's reply and escalation windows.
var helpdeskPack = deadline.Pack{ID: "helpdesk.sla", Version: "1", Holidays: []deadline.Date{day(4, 3)},
	Rules: []deadline.Rule{
		{ID: "reply", Trigger: "ticket_opened", Mode: deadline.Calendar, Days: 10},
		{ID: "notice", Trigger: "ticket_opened", Mode: deadline.Clear, Days: 10},
		{ID: "escalate", Trigger: "ticket_opened", Mode: deadline.Business, Days: 3},
		{ID: "review", Trigger: "ticket_opened", Mode: deadline.Calendar, MonthsAfter: 1, Roll: deadline.RollForward},
		{ID: "weekend", Trigger: "ticket_opened", Mode: deadline.Calendar, Days: 2, Roll: deadline.RollForward},
	}}

func TestCompute(t *testing.T) {
	tests := []struct {
		name    string
		pack    string
		rule    string
		trigger deadline.Date
		want    deadline.Date
		err     error
	}{
		{name: "calendar", pack: "helpdesk.sla", rule: "reply", trigger: day(3, 2), want: day(3, 12)},
		{name: "clear days", pack: "helpdesk.sla", rule: "notice", trigger: day(3, 2), want: day(3, 13)},
		{name: "business skips weekend", pack: "helpdesk.sla", rule: "escalate", trigger: day(3, 5), want: day(3, 10)},
		{name: "business skips holiday", pack: "helpdesk.sla", rule: "escalate", trigger: day(4, 1), want: day(4, 7)},
		{name: "months after rolls off Sunday", pack: "helpdesk.sla", rule: "review", trigger: day(2, 1), want: day(3, 2)},
		{name: "roll over Saturday", pack: "helpdesk.sla", rule: "weekend", trigger: day(3, 5), want: day(3, 9)},
		{name: "unknown pack", pack: "x", rule: "reply", err: deadline.ErrUnknownPack},
		{name: "unknown rule", pack: "helpdesk.sla", rule: "x", err: deadline.ErrUnknownRule},
	}
	f := New()
	if err := f.LoadPack(context.Background(), helpdeskPack); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Compute(context.Background(), tt.pack, tt.rule, tt.trigger, deadline.Subject{Kind: "ticket", ID: "t1"})
			if !errors.Is(err, tt.err) {
				t.Fatalf("Compute err = %v, want %v", err, tt.err)
			}
			if err == nil && (len(got) != 1 || got[0].Due != tt.want || got[0].PackVersion != "1") {
				t.Fatalf("Compute = %+v, want due %+v", got, tt.want)
			}
		})
	}
}

func TestLoadPack(t *testing.T) {
	tests := []struct {
		name string
		p    deadline.Pack
		err  error
	}{
		{name: "valid", p: helpdeskPack},
		{name: "no version", p: deadline.Pack{ID: "p"}, err: deadline.ErrInvalidPack},
		{name: "bad mode", p: deadline.Pack{ID: "p", Version: "1", Rules: []deadline.Rule{{ID: "r", Mode: "lunar"}}}, err: deadline.ErrInvalidPack},
		{name: "negative days", p: deadline.Pack{ID: "p", Version: "1", Rules: []deadline.Rule{{ID: "r", Mode: deadline.Calendar, Days: -1}}}, err: deadline.ErrInvalidPack},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := New().LoadPack(context.Background(), tt.p); !errors.Is(err, tt.err) {
				t.Fatalf("LoadPack err = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		name  string
		by    deadline.Principal
		twice bool
		err   error
	}{
		{name: "person confirms", by: deadline.Principal{Kind: "user", UserID: "u1"}},
		{name: "agent refused", by: deadline.Principal{Kind: "agent", RunID: "r1", ActingFor: "u1"}, err: deadline.ErrAgentCannotConfirm},
		{name: "confirm twice", by: deadline.Principal{Kind: "user", UserID: "u1"}, twice: true, err: deadline.ErrAlreadyConfirmed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New()
			id, err := f.Save(ctx, nil, deadline.Proposed{PackID: "p", RuleID: "r", Due: day(3, 2)})
			if err != nil {
				t.Fatal(err)
			}
			if tt.twice {
				if err := f.Confirm(ctx, nil, id, tt.by); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.Confirm(ctx, nil, id, tt.by); !errors.Is(err, tt.err) {
				t.Fatalf("Confirm err = %v, want %v", err, tt.err)
			}
			d, _ := f.Get(ctx, id)
			if wantConfirmed := tt.err == nil || tt.twice; (d.State == deadline.StateConfirmed) != wantConfirmed {
				t.Fatalf("state = %s", d.State)
			}
		})
	}
	if err := New().Confirm(context.Background(), nil, "nope", deadline.Principal{Kind: "user", UserID: "u"}); !errors.Is(err, deadline.ErrNotFound) {
		t.Fatalf("Confirm missing err = %v", err)
	}
}
