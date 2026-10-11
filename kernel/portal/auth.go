package portal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// ErrNoGuest is returned by OIDC sign-in when the verified identity maps to no guest.
var ErrNoGuest = errors.New("portal: no guest for identity")

// DefaultSessionTTL is how long a session lasts unless Shell.SessionTTL says otherwise.
const DefaultSessionTTL = time.Hour

// MaxTokenTTL caps a magic-link token's lifetime.
const MaxTokenTTL = 7 * 24 * time.Hour

// OIDCVerifier checks a raw ID token from the guest's identity provider and returns its
// issuer and subject. Signature, audience and expiry checks are the verifier's job.
type OIDCVerifier interface {
	Verify(ctx context.Context, rawIDToken string) (issuer, subject string, err error)
}

// GuestLookup maps a verified OIDC identity to a guest and its current grants.
type GuestLookup func(ctx context.Context, issuer, subject string) (Guest, error)

// Shell is the portal shell. This file holds guest identity, sign-in and sessions; page
// registration and Load live in pages.go.
//
// Tokens and session ids are 32 random bytes. Only their SHA-256 hashes reach the
// SessionStore, so neither a memory dump nor the database holds a usable credential.
type Shell struct {
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// SessionTTL is how long a session lasts; DefaultSessionTTL when zero.
	SessionTTL time.Duration
	// Verifier and Lookup enable SignInOIDC; both nil means OIDC is off.
	Verifier OIDCVerifier
	Lookup   GuestLookup

	// Store keeps sign-in tokens and sessions by hash; NewShell sets an in-memory store.
	// PGSessions keeps them in Postgres.
	Store SessionStore

	mu      sync.Mutex
	pages   map[string]PortalPage
	pageIDs map[string]bool
}

// NewShell returns an empty Shell with an in-memory SessionStore.
func NewShell() *Shell {
	return &Shell{
		Store: newMemSessions(),
		pages: map[string]PortalPage{}, pageIDs: map[string]bool{},
	}
}

func (s *Shell) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Shell) sessionTTL() time.Duration {
	if s.SessionTTL > 0 {
		return s.SessionTTL
	}
	return DefaultSessionTTL
}

// IssueToken creates a one-time magic-link token for a guest. The token is returned once
// and never logged; the shell keeps only its hash.
func (s *Shell) IssueToken(ctx context.Context, g Guest, ttl time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if g.ID == "" {
		return "", fmt.Errorf("issue token: guest id is required: %w", ErrBadToken)
	}
	if ttl <= 0 || ttl > MaxTokenTTL {
		return "", fmt.Errorf("issue token: ttl %s outside (0, %s]: %w", ttl, MaxTokenTTL, ErrBadToken)
	}
	tok, err := randomID()
	if err != nil {
		return "", fmt.Errorf("issue token: %w", err)
	}
	if err := s.Store.PutToken(ctx, hash(tok), cloneGuest(g), s.now().Add(ttl)); err != nil {
		return "", fmt.Errorf("issue token: %w", err)
	}
	return tok, nil
}

// SignIn exchanges a magic-link token for a session. A token works once, used or not.
func (s *Shell) SignIn(ctx context.Context, token string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	g, err := s.Store.TakeToken(ctx, hash(token), s.now())
	if err != nil {
		return Session{}, fmt.Errorf("sign-in: %w", err)
	}
	return s.newSession(ctx, g)
}

// SignInOIDC exchanges a verified OIDC ID token for a session. The guest and its grants
// come from Lookup at sign-in time.
func (s *Shell) SignInOIDC(ctx context.Context, rawIDToken string) (Session, error) {
	if s.Verifier == nil || s.Lookup == nil {
		return Session{}, fmt.Errorf("oidc sign-in is not configured: %w", ErrBadToken)
	}
	iss, sub, err := s.Verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Session{}, fmt.Errorf("oidc sign-in: %w: %v", ErrBadToken, err)
	}
	g, err := s.Lookup(ctx, iss, sub)
	if err != nil {
		return Session{}, fmt.Errorf("oidc sign-in: lookup: %w", err)
	}
	if g.ID == "" {
		return Session{}, fmt.Errorf("oidc sign-in: %w", ErrNoGuest)
	}
	return s.newSession(ctx, cloneGuest(g))
}

// Session returns the live session with this id.
func (s *Shell) Session(ctx context.Context, sessionID string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	g, exp, err := s.Store.Session(ctx, hash(sessionID), s.now())
	if err != nil {
		return Session{}, fmt.Errorf("session: %w", err)
	}
	return Session{ID: sessionID, Guest: cloneGuest(g), ExpiresAt: exp}, nil
}

// SignOut ends a session. Ending an unknown session is not an error.
func (s *Shell) SignOut(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.Store.EndSession(ctx, hash(sessionID), s.now()); err != nil {
		return fmt.Errorf("sign out: %w", err)
	}
	return nil
}

// Authorize returns the session's guest if the guest's grant lists subject. A guest sees
// only the subjects its grant lists.
func (s *Shell) Authorize(ctx context.Context, sessionID, subject string) (Guest, error) {
	sess, err := s.Session(ctx, sessionID)
	if err != nil {
		return Guest{}, err
	}
	if subject == "" || !slices.Contains(sess.Guest.Grants, subject) {
		return Guest{}, ErrNotGranted
	}
	return sess.Guest, nil
}

func (s *Shell) newSession(ctx context.Context, g Guest) (Session, error) {
	id, err := randomID()
	if err != nil {
		return Session{}, fmt.Errorf("new session: %w", err)
	}
	exp := s.now().Add(s.sessionTTL())
	if err := s.Store.PutSession(ctx, hash(id), cloneGuest(g), exp); err != nil {
		return Session{}, fmt.Errorf("new session: %w", err)
	}
	return Session{ID: id, Guest: cloneGuest(g), ExpiresAt: exp}, nil
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

func cloneGuest(g Guest) Guest {
	g.Grants = slices.Clone(g.Grants)
	return g
}
