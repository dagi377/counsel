// Package portalfake is an in-memory implementation of the kernel/portal contract for tests.
package portalfake

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/portal"
)

var _ portal.Portal = (*Fake)(nil)

type token struct {
	guest   portal.Guest
	expires time.Time
}

// Fake keeps pages, tokens and sessions in memory. Tokens and session ids are counters,
// not secrets. A Fake is safe for concurrent use.
type Fake struct {
	mu       sync.Mutex
	now      func() time.Time
	seq      int
	pages    map[string]portal.PortalPage // by route
	ids      map[string]bool
	tokens   map[string]token
	sessions map[string]portal.Session
	// SessionTTL is how long a session lasts; one hour by default.
	SessionTTL time.Duration
}

// New returns an empty Fake using now as its clock.
func New(now func() time.Time) *Fake {
	if now == nil {
		now = time.Now
	}
	return &Fake{
		now: now, pages: map[string]portal.PortalPage{}, ids: map[string]bool{},
		tokens: map[string]token{}, sessions: map[string]portal.Session{}, SessionTTL: time.Hour,
	}
}

// RegisterPage adds a page.
func (f *Fake) RegisterPage(p portal.PortalPage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.pages[p.Route]; ok || f.ids[p.ID] {
		return fmt.Errorf("register %q: %w", p.ID, portal.ErrDuplicatePage)
	}
	f.pages[p.Route] = p
	f.ids[p.ID] = true
	return nil
}

// IssueToken creates a one-time token.
func (f *Fake) IssueToken(_ context.Context, g portal.Guest, ttl time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	t := "tok-" + strconv.Itoa(f.seq)
	f.tokens[t] = token{guest: g, expires: f.now().Add(ttl)}
	return t, nil
}

// SignIn exchanges a token for a session; the token is consumed.
func (f *Fake) SignIn(_ context.Context, tok string) (portal.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tokens[tok]
	delete(f.tokens, tok)
	if !ok || !f.now().Before(t.expires) {
		return portal.Session{}, portal.ErrBadToken
	}
	f.seq++
	s := portal.Session{ID: "sess-" + strconv.Itoa(f.seq), Guest: t.guest, ExpiresAt: f.now().Add(f.SessionTTL)}
	f.sessions[s.ID] = s
	return s, nil
}

// Load runs a registered page for a granted subject.
func (f *Fake) Load(ctx context.Context, sessionID, route, subject string) (any, error) {
	f.mu.Lock()
	s, ok := f.sessions[sessionID]
	p, found := f.pages[route]
	now := f.now()
	f.mu.Unlock()
	if !ok || !now.Before(s.ExpiresAt) {
		return nil, portal.ErrNoSession
	}
	if !found {
		return nil, fmt.Errorf("load %q: %w", route, portal.ErrPageNotFound)
	}
	if !slices.Contains(s.Guest.Grants, subject) {
		return nil, portal.ErrNotGranted
	}
	v, err := p.Load(ctx, s.Guest, subject)
	if err != nil {
		return nil, fmt.Errorf("load %q: %w", route, err)
	}
	return v, nil
}
