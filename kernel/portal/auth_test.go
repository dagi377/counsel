package portal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/portal"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newShell() (*portal.Shell, *clock) {
	c := &clock{t: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)}
	s := portal.NewShell()
	s.Now = c.now
	s.SessionTTL = time.Hour
	return s, c
}

var guest = portal.Guest{ID: "g1", Grants: []string{"s1", "s2"}}

func TestMagicLinkSignIn(t *testing.T) {
	tests := []struct {
		name    string
		ttl     time.Duration
		advance time.Duration
		reuse   bool
		badTok  bool
		issue   error
		want    error
	}{
		{name: "valid token", ttl: time.Minute},
		{name: "expired token", ttl: time.Minute, advance: time.Minute, want: portal.ErrBadToken},
		{name: "token works once", ttl: time.Minute, reuse: true, want: portal.ErrBadToken},
		{name: "unknown token", ttl: time.Minute, badTok: true, want: portal.ErrBadToken},
		{name: "zero ttl", ttl: 0, issue: portal.ErrBadToken},
		{name: "ttl over cap", ttl: portal.MaxTokenTTL + time.Second, issue: portal.ErrBadToken},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, c := newShell()
			tok, err := s.IssueToken(ctx, guest, tc.ttl)
			if !errors.Is(err, tc.issue) {
				t.Fatalf("issue err = %v, want %v", err, tc.issue)
			}
			if err != nil {
				return
			}
			c.t = c.t.Add(tc.advance)
			if tc.reuse {
				if _, err := s.SignIn(ctx, tok); err != nil {
					t.Fatal(err)
				}
			}
			if tc.badTok {
				tok += "x"
			}
			sess, err := s.SignIn(ctx, tok)
			if !errors.Is(err, tc.want) {
				t.Fatalf("sign-in err = %v, want %v", err, tc.want)
			}
			if err != nil {
				return
			}
			if sess.ID == "" || sess.ID == tok || sess.Guest.ID != "g1" || !sess.ExpiresAt.Equal(c.t.Add(time.Hour)) {
				t.Fatalf("session = %+v", sess)
			}
		})
	}
}

func TestTokensAreUnique(t *testing.T) {
	s, _ := newShell()
	seen := map[string]bool{}
	for range 50 {
		tok, err := s.IssueToken(context.Background(), guest, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok] || len(tok) < 40 {
			t.Fatalf("weak or repeated token %q", tok)
		}
		seen[tok] = true
	}
}

type verifier struct {
	iss, sub string
	err      error
}

func (v verifier) Verify(context.Context, string) (string, string, error) { return v.iss, v.sub, v.err }

func TestOIDCSignIn(t *testing.T) {
	lookup := func(_ context.Context, iss, sub string) (portal.Guest, error) {
		if iss == "https://idp" && sub == "alice" {
			return guest, nil
		}
		return portal.Guest{}, nil
	}
	tests := []struct {
		name   string
		v      portal.OIDCVerifier
		lookup portal.GuestLookup
		want   error
	}{
		{"verified and known", verifier{iss: "https://idp", sub: "alice"}, lookup, nil},
		{"verify fails", verifier{err: errors.New("bad signature")}, lookup, portal.ErrBadToken},
		{"unknown identity", verifier{iss: "https://idp", sub: "mallory"}, lookup, portal.ErrNoGuest},
		{"not configured", nil, nil, portal.ErrBadToken},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newShell()
			s.Verifier, s.Lookup = tc.v, tc.lookup
			sess, err := s.SignInOIDC(context.Background(), "raw")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err == nil && sess.Guest.ID != "g1" {
				t.Fatalf("session = %+v", sess)
			}
		})
	}
}

func TestSessionAndGrants(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		advance time.Duration
		signOut bool
		badID   bool
		want    error
	}{
		{name: "granted subject", subject: "s1"},
		{name: "second granted subject", subject: "s2"},
		{name: "subject not in grant", subject: "s3", want: portal.ErrNotGranted},
		{name: "empty subject", subject: "", want: portal.ErrNotGranted},
		{name: "expired session", subject: "s1", advance: time.Hour, want: portal.ErrNoSession},
		{name: "signed out", subject: "s1", signOut: true, want: portal.ErrNoSession},
		{name: "unknown session", subject: "s1", badID: true, want: portal.ErrNoSession},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, c := newShell()
			tok, err := s.IssueToken(ctx, guest, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			sess, err := s.SignIn(ctx, tok)
			if err != nil {
				t.Fatal(err)
			}
			c.t = c.t.Add(tc.advance)
			if tc.signOut {
				if err := s.SignOut(ctx, sess.ID); err != nil {
					t.Fatal(err)
				}
			}
			id := sess.ID
			if tc.badID {
				id = tok
			}
			g, err := s.Authorize(ctx, id, tc.subject)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err == nil && g.ID != "g1" {
				t.Fatalf("guest = %+v", g)
			}
		})
	}
}

func TestGrantsAreCopied(t *testing.T) {
	ctx := context.Background()
	s, _ := newShell()
	g := portal.Guest{ID: "g1", Grants: []string{"s1"}}
	tok, err := s.IssueToken(ctx, g, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	g.Grants[0] = "s9" // caller mutates its slice after issuing
	sess, err := s.SignIn(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	sess.Guest.Grants[0] = "s9" // caller mutates the returned session
	if _, err := s.Authorize(ctx, sess.ID, "s9"); !errors.Is(err, portal.ErrNotGranted) {
		t.Fatalf("mutated grant leaked in: %v", err)
	}
	if _, err := s.Authorize(ctx, sess.ID, "s1"); err != nil {
		t.Fatal(err)
	}
}
