package portalfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/portal"
)

func TestLoad(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		route   string
		subject string
		advance time.Duration // clock moves after sign-in
		reuse   bool          // sign in twice with the same token
		want    any
		err     error
	}{
		{name: "granted subject", route: "/docs", subject: "s1", want: "docs:s1"},
		{name: "subject not in grant", route: "/docs", subject: "s2", err: portal.ErrNotGranted},
		{name: "unregistered page", route: "/nope", subject: "s1", err: portal.ErrPageNotFound},
		{name: "expired session", route: "/docs", subject: "s1", advance: 2 * time.Hour, err: portal.ErrNoSession},
		{name: "token works once", route: "/docs", subject: "s1", reuse: true, err: portal.ErrBadToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := start
			f := New(func() time.Time { return now })
			err := f.RegisterPage(portal.PortalPage{ID: "mod.docs", Route: "/docs", Load: func(_ context.Context, _ portal.Guest, s string) (any, error) {
				return "docs:" + s, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			tok, _ := f.IssueToken(ctx, portal.Guest{ID: "g1", Grants: []string{"s1"}}, 15*time.Minute)
			s, err := f.SignIn(ctx, tok)
			if err != nil {
				t.Fatal(err)
			}
			if tt.reuse {
				if _, err := f.SignIn(ctx, tok); !errors.Is(err, tt.err) {
					t.Fatalf("second SignIn err = %v, want %v", err, tt.err)
				}
				return
			}
			now = now.Add(tt.advance)
			got, err := f.Load(ctx, s.ID, tt.route, tt.subject)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegisterAndSignIn(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		run  func(f *Fake) error
		err  error
	}{
		{name: "duplicate route", run: func(f *Fake) error {
			_ = f.RegisterPage(portal.PortalPage{ID: "a", Route: "/x"})
			return f.RegisterPage(portal.PortalPage{ID: "b", Route: "/x"})
		}, err: portal.ErrDuplicatePage},
		{name: "duplicate id", run: func(f *Fake) error {
			_ = f.RegisterPage(portal.PortalPage{ID: "a", Route: "/x"})
			return f.RegisterPage(portal.PortalPage{ID: "a", Route: "/y"})
		}, err: portal.ErrDuplicatePage},
		{name: "unknown token", run: func(f *Fake) error {
			_, err := f.SignIn(ctx, "tok-404")
			return err
		}, err: portal.ErrBadToken},
		{name: "expired token", run: func(f *Fake) error {
			tok, _ := f.IssueToken(ctx, portal.Guest{ID: "g"}, 0)
			_, err := f.SignIn(ctx, tok)
			return err
		}, err: portal.ErrBadToken},
		{name: "unknown session", run: func(f *Fake) error {
			_, err := f.Load(ctx, "sess-404", "/x", "s")
			return err
		}, err: portal.ErrNoSession},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(New(func() time.Time { return now })); !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
		})
	}
}
