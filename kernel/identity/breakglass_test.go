package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/audit/auditfake"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

var (
	bgUser    = identity.Principal{Kind: registry.PrincipalUser, UserID: "u1"}
	bgSubject = identity.Subject{Kind: "desk.ticket", ID: "t1"}
)

func bgRequest(mut func(*identity.BreakGlassRequest)) identity.BreakGlassRequest {
	r := identity.BreakGlassRequest{By: bgUser, Subject: bgSubject, Reason: "urgent", Explanation: "on call", Duration: 2 * time.Hour}
	if mut != nil {
		mut(&r)
	}
	return r
}

func TestBreakGlassOpenValidation(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*identity.BreakGlassRequest)
		ok   bool
	}{
		{"valid", nil, true},
		{"max duration", func(r *identity.BreakGlassRequest) { r.Duration = identity.BreakGlassMax }, true},
		{"over max", func(r *identity.BreakGlassRequest) { r.Duration = identity.BreakGlassMax + time.Second }, false},
		{"zero duration", func(r *identity.BreakGlassRequest) { r.Duration = 0 }, false},
		{"agent", func(r *identity.BreakGlassRequest) {
			r.By = identity.Principal{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: "u1"}
		}, false},
		{"guest", func(r *identity.BreakGlassRequest) {
			r.By = identity.Principal{Kind: registry.PrincipalGuest, UserID: "g1"}
		}, false},
		{"no reason", func(r *identity.BreakGlassRequest) { r.Reason = " " }, false},
		{"no explanation", func(r *identity.BreakGlassRequest) { r.Explanation = "" }, false},
		{"no subject", func(r *identity.BreakGlassRequest) { r.Subject = identity.Subject{} }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := auditfake.New()
			bg := identity.NewBreakGlass(clockfake.New(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), time.UTC), log)
			_, err := bg.Open(context.Background(), nil, bgRequest(tc.mut))
			if tc.ok != (err == nil) {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
			if !tc.ok && !errors.Is(err, identity.ErrInvalidBreakGlass) {
				t.Fatalf("err = %v, want ErrInvalidBreakGlass", err)
			}
			wantEvents := 0
			if tc.ok {
				wantEvents = 1
			}
			if n := len(log.Events()); n != wantEvents {
				t.Fatalf("audit events = %d, want %d", n, wantEvents)
			}
		})
	}
}

func TestBreakGlassTimeBoxAndExpiry(t *testing.T) {
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		advance     time.Duration
		active      bool
		wantExpired int
	}{
		{"just opened", 0, true, 0},
		{"one second before end", 2*time.Hour - time.Second, true, 0},
		{"at end", 2 * time.Hour, false, 1},
		{"long after", 30 * time.Hour, false, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			clk := clockfake.New(start, time.UTC)
			log := auditfake.New("grant_id", "starts_at", "ends_at", "user_id")
			bg := identity.NewBreakGlass(clk, log)
			g, err := bg.Open(ctx, nil, bgRequest(nil))
			if err != nil {
				t.Fatal(err)
			}
			if !g.EndsAt.Equal(start.Add(2 * time.Hour)) {
				t.Fatalf("EndsAt = %s", g.EndsAt)
			}
			ev := log.Events()[0]
			if ev.Action != identity.ActionBreakGlassOpen || ev.ActorID != "u1" || ev.Subject != bgSubject {
				t.Fatalf("open event = %+v", ev)
			}
			if _, leaked := ev.Detail["reason"]; leaked || ev.Detail["grant_id"] != g.ID {
				t.Fatalf("open detail = %v", ev.Detail)
			}

			clk.Advance(tc.advance)
			if _, ok := bg.Active(ctx, "u1", bgSubject); ok != tc.active {
				t.Fatalf("Active = %v, want %v", ok, tc.active)
			}
			if _, ok := bg.Active(ctx, "u2", bgSubject); ok {
				t.Fatal("grant leaked to another user")
			}
			n, err := bg.Expire(ctx, nil)
			if err != nil || n != tc.wantExpired {
				t.Fatalf("Expire = %d, %v; want %d", n, err, tc.wantExpired)
			}
			if n, _ := bg.Expire(ctx, nil); n != 0 {
				t.Fatalf("second Expire = %d, want 0", n)
			}
			events := log.Events()
			if len(events) != 1+tc.wantExpired {
				t.Fatalf("audit events = %d", len(events))
			}
			if tc.wantExpired == 1 {
				last := events[len(events)-1]
				if last.Action != identity.ActionBreakGlassExpire || last.ActorKind != "system" || last.Detail["user_id"] != "u1" {
					t.Fatalf("expire event = %+v", last)
				}
				if !bg.Grants(ctx)[0].Expired {
					t.Fatal("grant not marked expired")
				}
			}
			if err := log.Verify(ctx, 1, 1); err != nil {
				t.Fatalf("chain: %v", err)
			}
		})
	}
}
