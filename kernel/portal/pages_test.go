package portal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/portal"
)

func echoLoader(_ context.Context, g portal.Guest, subject string) (any, error) {
	return g.ID + ":" + subject, nil
}

func TestRegisterPage(t *testing.T) {
	ok := portal.PortalPage{ID: "desk.status", Route: "/status", Capability: "desk.view", Load: echoLoader}
	tests := []struct {
		name string
		page func(p portal.PortalPage) portal.PortalPage
		want error
	}{
		{name: "valid", page: func(p portal.PortalPage) portal.PortalPage { return p }},
		{name: "route with param", page: func(p portal.PortalPage) portal.PortalPage { p.Route = "/status/{id}"; return p }},
		{name: "id not module-prefixed", page: func(p portal.PortalPage) portal.PortalPage { p.ID = "status"; return p }, want: portal.ErrInvalidPage},
		{name: "route without slash", page: func(p portal.PortalPage) portal.PortalPage { p.Route = "status"; return p }, want: portal.ErrInvalidPage},
		{name: "no capability", page: func(p portal.PortalPage) portal.PortalPage { p.Capability = ""; return p }, want: portal.ErrInvalidPage},
		{name: "no loader", page: func(p portal.PortalPage) portal.PortalPage { p.Load = nil; return p }, want: portal.ErrInvalidPage},
		{name: "duplicate id", page: func(p portal.PortalPage) portal.PortalPage { p.Route = "/other"; return p }, want: portal.ErrDuplicatePage},
		{name: "duplicate route", page: func(p portal.PortalPage) portal.PortalPage { p.ID = "desk.other"; return p }, want: portal.ErrDuplicatePage},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newShell()
			if tc.want == portal.ErrDuplicatePage {
				if err := s.RegisterPage(ok); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.RegisterPage(tc.page(ok)); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestLoadPage(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		route     string
		subject   string
		badSess   bool
		advance   time.Duration
		want      any
		wantErr   error
		wantCalls int
	}{
		{name: "registered page, granted subject", route: "/status", subject: "s1", want: "g1:s1", wantCalls: 1},
		{name: "second granted subject", route: "/status", subject: "s2", want: "g1:s2", wantCalls: 1},
		{name: "unregistered page is 404", route: "/missing", subject: "s1", wantErr: portal.ErrPageNotFound},
		{name: "subject outside grant", route: "/status", subject: "s3", wantErr: portal.ErrNotGranted},
		{name: "empty subject", route: "/status", subject: "", wantErr: portal.ErrNotGranted},
		{name: "unknown session", route: "/status", subject: "s1", badSess: true, wantErr: portal.ErrNoSession},
		{name: "unknown session does not reveal 404", route: "/missing", subject: "s1", badSess: true, wantErr: portal.ErrNoSession},
		{name: "expired session", route: "/status", subject: "s1", advance: time.Hour, wantErr: portal.ErrNoSession},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, c := newShell()
			calls := 0
			err := s.RegisterPage(portal.PortalPage{ID: "desk.status", Route: "/status", Capability: "desk.view",
				Load: func(ctx context.Context, g portal.Guest, subject string) (any, error) {
					calls++
					return echoLoader(ctx, g, subject)
				}})
			if err != nil {
				t.Fatal(err)
			}
			tok, err := s.IssueToken(ctx, guest, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			sess, err := s.SignIn(ctx, tok)
			if err != nil {
				t.Fatal(err)
			}
			id := sess.ID
			if tc.badSess {
				id = "nope"
			}
			c.t = c.t.Add(tc.advance)
			got, err := s.Load(ctx, id, tc.route, tc.subject)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got = %v, want %v", got, tc.want)
			}
			if calls != tc.wantCalls {
				t.Fatalf("loader calls = %d, want %d", calls, tc.wantCalls)
			}
		})
	}
}

func TestLoaderErrorPropagates(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")
	s, _ := newShell()
	if err := s.RegisterPage(portal.PortalPage{ID: "desk.status", Route: "/status", Capability: "desk.view",
		Load: func(context.Context, portal.Guest, string) (any, error) { return nil, boom }}); err != nil {
		t.Fatal(err)
	}
	tok, _ := s.IssueToken(ctx, guest, time.Minute)
	sess, _ := s.SignIn(ctx, tok)
	if _, err := s.Load(ctx, sess.ID, "/status", "s1"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
