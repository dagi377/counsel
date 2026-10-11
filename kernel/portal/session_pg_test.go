package portal_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/portal"
)

// tokenRow is one scripted platform.portal_token row; sessionRow one core.user_session row.
type tokenRow struct {
	hash, kind, guest string
	grants            []string
	sessionID         string
	expires           time.Time
	used              bool
}

type sessionRow struct {
	id, user, platform string
	ended              bool
}

// portalTables answers PGSessions' statements from scripted rows and records every argument,
// so a test can check that no raw token or session id reaches the database.
type portalTables struct {
	pgx.Tx
	mu       sync.Mutex
	tokens   []*tokenRow
	sessions []*sessionRow
	args     []string
	fail     error
}

func (f *portalTables) record(args []any) {
	for _, a := range args {
		f.args = append(f.args, fmt.Sprint(a))
	}
}

func (f *portalTables) token(hash, kind string) *tokenRow {
	for _, r := range f.tokens {
		if r.hash == hash && r.kind == kind {
			return r
		}
	}
	return nil
}

func (f *portalTables) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(args)
	if f.fail != nil {
		return pgconn.CommandTag{}, f.fail
	}
	switch {
	case strings.Contains(sql, "'sign_in', $2::uuid"):
		f.tokens = append(f.tokens, &tokenRow{hash: args[0].(string), kind: "sign_in", guest: args[1].(string),
			grants: slices.Clone(args[2].([]string)), expires: args[3].(time.Time)})
	case strings.Contains(sql, "'session', $2::uuid"):
		f.tokens = append(f.tokens, &tokenRow{hash: args[0].(string), kind: "session", guest: args[1].(string),
			grants: slices.Clone(args[2].([]string)), sessionID: args[3].(string), expires: args[4].(time.Time)})
	case strings.HasPrefix(sql, "UPDATE core.user_session"):
		if t := f.token(args[0].(string), "session"); t != nil {
			for _, s := range f.sessions {
				if s.id == t.sessionID {
					s.ended = true
				}
			}
		}
	default:
		return pgconn.CommandTag{}, errors.New("portalTables: unexpected exec")
	}
	return pgconn.NewCommandTag("OK"), nil
}

type portalRow struct {
	vals []any
	err  error
}

func (r portalRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return fmt.Errorf("portalRow: %d dest for %d values", len(dest), len(r.vals))
	}
	for i, v := range r.vals {
		switch d := dest[i].(type) {
		case *string:
			*d = v.(string)
		case *[]string:
			*d = slices.Clone(v.([]string))
		case *bool:
			*d = v.(bool)
		case *time.Time:
			*d = v.(time.Time)
		}
	}
	return nil
}

func (f *portalTables) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(args)
	if f.fail != nil {
		return portalRow{err: f.fail}
	}
	switch {
	case strings.HasPrefix(sql, "UPDATE platform.portal_token"): // take token
		now := args[1].(time.Time)
		t := f.token(args[0].(string), "sign_in")
		if t == nil || t.used {
			return portalRow{err: pgx.ErrNoRows}
		}
		t.used = true
		return portalRow{vals: []any{t.guest, t.grants, t.expires.After(now)}}
	case strings.HasPrefix(sql, "INSERT INTO core.user_session"):
		s := &sessionRow{id: fmt.Sprintf("sess-%d", len(f.sessions)+1), user: args[0].(string), platform: "portal"}
		f.sessions = append(f.sessions, s)
		return portalRow{vals: []any{s.id}}
	case strings.HasPrefix(sql, "SELECT t.guest_id"): // live session
		now := args[1].(time.Time)
		t := f.token(args[0].(string), "session")
		if t == nil || !t.expires.After(now) {
			return portalRow{err: pgx.ErrNoRows}
		}
		for _, s := range f.sessions {
			if s.id == t.sessionID && !s.ended {
				return portalRow{vals: []any{t.guest, t.grants, t.expires}}
			}
		}
		return portalRow{err: pgx.ErrNoRows}
	}
	return portalRow{err: errors.New("portalTables: unexpected query")}
}

type portalDB struct{ tx *portalTables }

func (d portalDB) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, d.tx)
}
func (portalDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (portalDB) Ping(context.Context) error                     { return nil }
func (portalDB) Close()                                         {}

// shellStores builds a Shell over each SessionStore, with the auth_test.go clock and TTL.
func shellStores() []struct {
	name string
	new  func() (*portal.Shell, *clock, *portalTables)
} {
	return []struct {
		name string
		new  func() (*portal.Shell, *clock, *portalTables)
	}{
		{"memory", func() (*portal.Shell, *clock, *portalTables) { s, c := newShell(); return s, c, nil }},
		{"postgres", func() (*portal.Shell, *clock, *portalTables) {
			s, c := newShell()
			tbl := &portalTables{}
			s.Store = portal.PGSessions{DB: portalDB{tbl}}
			return s, c, tbl
		}},
	}
}

// TestStoresMagicLinkSignIn runs the TestMagicLinkSignIn cases on both stores.
func TestStoresMagicLinkSignIn(t *testing.T) {
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
	for _, st := range shellStores() {
		for _, tc := range tests {
			t.Run(st.name+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				s, c, _ := st.new()
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
}

// TestStoresSessionAndGrants runs the TestSessionAndGrants cases on both stores, plus a
// revoked session (its user_session ended by an admin) on Postgres.
func TestStoresSessionAndGrants(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		advance time.Duration
		signOut bool
		revoke  bool
		badID   bool
		want    error
	}{
		{name: "granted subject", subject: "s1"},
		{name: "second granted subject", subject: "s2"},
		{name: "subject not in grant", subject: "s3", want: portal.ErrNotGranted},
		{name: "empty subject", subject: "", want: portal.ErrNotGranted},
		{name: "expired session", subject: "s1", advance: time.Hour, want: portal.ErrNoSession},
		{name: "signed out", subject: "s1", signOut: true, want: portal.ErrNoSession},
		{name: "revoked", subject: "s1", revoke: true, want: portal.ErrNoSession},
		{name: "unknown session", subject: "s1", badID: true, want: portal.ErrNoSession},
	}
	for _, st := range shellStores() {
		for _, tc := range tests {
			t.Run(st.name+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				s, c, tbl := st.new()
				if tc.revoke && tbl == nil {
					t.Skip("revocation ends the user_session row; memory has none")
				}
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
				if tc.revoke {
					tbl.sessions[0].ended = true
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
				if tbl != nil && (len(tbl.sessions) != 1 || tbl.sessions[0].platform != "portal" || tbl.sessions[0].user != "g1") {
					t.Fatalf("user_session rows = %+v", tbl.sessions)
				}
			})
		}
	}
}

// TestPGSessionsKeepNoRawSecrets: only hashes reach the database, and neither errors nor
// logs carry a raw token or session id.
func TestPGSessionsKeepNoRawSecrets(t *testing.T) {
	boom := errors.New("connection reset")
	cases := []struct {
		name string
		fail error // set after sign-in, before the session is read
	}{
		{"happy path", nil},
		{"store error", boom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(prev) })

			ctx := context.Background()
			s, _ := newShell()
			tbl := &portalTables{}
			s.Store = portal.PGSessions{DB: portalDB{tbl}}
			tok, err := s.IssueToken(ctx, guest, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			sess, err := s.SignIn(ctx, tok)
			if err != nil {
				t.Fatal(err)
			}
			tbl.fail = tc.fail
			_, aerr := s.Authorize(ctx, sess.ID, "s1")
			serr := s.SignOut(ctx, sess.ID)
			if !errors.Is(aerr, tc.fail) || !errors.Is(serr, tc.fail) {
				t.Fatalf("errors = %v, %v; want %v", aerr, serr, tc.fail)
			}
			for _, raw := range []string{tok, sess.ID} {
				for _, a := range tbl.args {
					if strings.Contains(a, raw) {
						t.Fatalf("raw credential reached the database: %q", a)
					}
				}
				for _, e := range []error{aerr, serr} {
					if e != nil && strings.Contains(e.Error(), raw) {
						t.Fatalf("raw credential in error: %v", e)
					}
				}
				if strings.Contains(logs.String(), raw) {
					t.Fatalf("raw credential in logs: %q", logs.String())
				}
			}
			for _, r := range tbl.tokens {
				if len(r.hash) != 64 || strings.Trim(r.hash, "0123456789abcdef") != "" {
					t.Fatalf("stored key %q is not a sha256 hex", r.hash)
				}
			}
		})
	}
}
