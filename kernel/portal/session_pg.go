package portal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// SessionStore keeps magic-link tokens and guest sessions for Shell. Every key is the
// SHA-256 hex of the raw token or session id; a store never sees the raw value.
type SessionStore interface {
	// PutToken keeps a sign-in token until expires.
	PutToken(ctx context.Context, tokenHash string, g Guest, expires time.Time) error
	// TakeToken consumes a token and returns its guest. An unknown, used or expired token
	// is ErrBadToken; a token is consumed by the first attempt either way.
	TakeToken(ctx context.Context, tokenHash string, now time.Time) (Guest, error)
	// PutSession starts a session that lasts until expires.
	PutSession(ctx context.Context, sessionHash string, g Guest, expires time.Time) error
	// Session returns a live session's guest and expiry. An unknown, expired, signed-out or
	// revoked session is ErrNoSession.
	Session(ctx context.Context, sessionHash string, now time.Time) (Guest, time.Time, error)
	// EndSession signs a session out. Ending an unknown session is not an error.
	EndSession(ctx context.Context, sessionHash string, now time.Time) error
}

// memSessions is the in-memory SessionStore NewShell uses.
type memSessions struct {
	mu       sync.Mutex
	tokens   map[string]memEntry
	sessions map[string]memEntry
}

type memEntry struct {
	guest   Guest
	expires time.Time
}

func newMemSessions() *memSessions {
	return &memSessions{tokens: map[string]memEntry{}, sessions: map[string]memEntry{}}
}

func (m *memSessions) PutToken(_ context.Context, h string, g Guest, exp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[h] = memEntry{cloneGuest(g), exp}
	return nil
}

func (m *memSessions) TakeToken(_ context.Context, h string, now time.Time) (Guest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.tokens[h]
	delete(m.tokens, h)
	if !ok || !now.Before(e.expires) {
		return Guest{}, ErrBadToken
	}
	return cloneGuest(e.guest), nil
}

func (m *memSessions) PutSession(_ context.Context, h string, g Guest, exp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[h] = memEntry{cloneGuest(g), exp}
	return nil
}

func (m *memSessions) Session(_ context.Context, h string, now time.Time) (Guest, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.sessions[h]
	if !ok {
		return Guest{}, time.Time{}, ErrNoSession
	}
	if !now.Before(e.expires) {
		delete(m.sessions, h)
		return Guest{}, time.Time{}, ErrNoSession
	}
	return cloneGuest(e.guest), e.expires, nil
}

func (m *memSessions) EndSession(_ context.Context, h string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, h)
	return nil
}

var _ SessionStore = PGSessions{}

// PGSessions is a SessionStore over platform.portal_token and core.user_session
// (platform 'portal'). It stores only token and session-id hashes. A session is live while
// its token row has not expired and its user_session has not ended, so revoking the
// user_session (ended_at set) ends the portal session too.
type PGSessions struct {
	DB db.DB
}

const (
	sqlPutToken = `INSERT INTO platform.portal_token (token_hash, kind, guest_id, grants, expires_at)
VALUES ($1, 'sign_in', $2::uuid, $3, $4)`
	// sqlTakeToken consumes the token whether or not it is still valid, then reports whether
	// it was: a token works once, used or not.
	sqlTakeToken = `UPDATE platform.portal_token SET used_at = $2
WHERE token_hash = $1 AND kind = 'sign_in' AND used_at IS NULL
RETURNING guest_id::text, grants, expires_at > $2`
	sqlStartUserSession = `INSERT INTO core.user_session (user_id, platform) VALUES ($1::uuid, 'portal') RETURNING id::text`
	sqlPutSession       = `INSERT INTO platform.portal_token (token_hash, kind, guest_id, grants, session_id, expires_at)
VALUES ($1, 'session', $2::uuid, $3, $4::uuid, $5)`
	sqlSession = `SELECT t.guest_id::text, t.grants, t.expires_at
FROM platform.portal_token t JOIN core.user_session s ON s.id = t.session_id
WHERE t.token_hash = $1 AND t.kind = 'session' AND t.expires_at > $2 AND s.ended_at IS NULL`
	sqlEndSession = `UPDATE core.user_session SET ended_at = $2, end_reason = 'logout'
WHERE ended_at IS NULL AND id = (SELECT session_id FROM platform.portal_token WHERE token_hash = $1 AND kind = 'session')`
)

// PutToken implements SessionStore.
func (p PGSessions) PutToken(ctx context.Context, h string, g Guest, exp time.Time) error {
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sqlPutToken, h, g.ID, grantsOf(g), exp)
		return err
	})
	if err != nil {
		return fmt.Errorf("put token: %w", err)
	}
	return nil
}

// TakeToken implements SessionStore.
func (p PGSessions) TakeToken(ctx context.Context, h string, now time.Time) (Guest, error) {
	var (
		g     Guest
		valid bool
	)
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlTakeToken, h, now).Scan(&g.ID, &g.Grants, &valid)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Guest{}, ErrBadToken
	case err != nil:
		return Guest{}, fmt.Errorf("take token: %w", err)
	case !valid:
		return Guest{}, ErrBadToken
	}
	return g, nil
}

// PutSession implements SessionStore. The user_session row and the hashed handle commit
// together.
func (p PGSessions) PutSession(ctx context.Context, h string, g Guest, exp time.Time) error {
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var sid string
		if err := tx.QueryRow(ctx, sqlStartUserSession, g.ID).Scan(&sid); err != nil {
			return fmt.Errorf("user session: %w", err)
		}
		_, err := tx.Exec(ctx, sqlPutSession, h, g.ID, grantsOf(g), sid, exp)
		return err
	})
	if err != nil {
		return fmt.Errorf("put session: %w", err)
	}
	return nil
}

// Session implements SessionStore.
func (p PGSessions) Session(ctx context.Context, h string, now time.Time) (Guest, time.Time, error) {
	var (
		g   Guest
		exp time.Time
	)
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlSession, h, now).Scan(&g.ID, &g.Grants, &exp)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Guest{}, time.Time{}, ErrNoSession
	case err != nil:
		return Guest{}, time.Time{}, fmt.Errorf("session: %w", err)
	}
	return g, exp, nil
}

// EndSession implements SessionStore.
func (p PGSessions) EndSession(ctx context.Context, h string, now time.Time) error {
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sqlEndSession, h, now)
		return err
	})
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

func grantsOf(g Guest) []string {
	if g.Grants == nil {
		return []string{}
	}
	return slices.Clone(g.Grants)
}
