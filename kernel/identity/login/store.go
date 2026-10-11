package login

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// Session is one member session row.
type Session struct {
	ID         string
	UserID     string
	StartedAt  time.Time
	LastSeenAt time.Time
	// Ended is true once the session was signed out, timed out, expired or revoked.
	Ended bool
	// UserActive is false once the user is suspended or offboarded.
	UserActive bool
	// Lifetime and Idle are the firm's sign-in policy limits (core.sign_in_policy), read
	// with the session; zero means no policy row, and the Config limits apply.
	Lifetime time.Duration
	Idle     time.Duration
}

// End reasons, as core.user_session.end_reason allows them.
const (
	EndLogout  = "logout"
	EndIdle    = "idle"
	EndExpired = "expired"
	EndRevoked = "revoked"
)

// Store keeps member sessions.
type Store interface {
	// Start opens a web session for userID at now and returns its id.
	Start(ctx context.Context, userID, ip, device string, now time.Time) (string, error)
	// Get returns a session; an unknown id is ErrNoSession.
	Get(ctx context.Context, id string) (Session, error)
	// Touch records activity at now on an open session.
	Touch(ctx context.Context, id string, now time.Time) error
	// End closes an open session with reason; ending a closed or unknown one is not an error.
	End(ctx context.Context, id string, now time.Time, reason string) error
}

// Users maps a verified email to an active human member.
type Users interface {
	// ActiveByEmail returns the user id, or ErrNoUser.
	ActiveByEmail(ctx context.Context, email string) (string, error)
}

var (
	_ Store = PGStore{}
	_ Users = PGStore{}
)

// PGStore is the Store and Users over core.user_session (platform 'web') and core.app_user.
type PGStore struct {
	DB db.DB
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const (
	sqlStart = `INSERT INTO core.user_session (user_id, platform, device, ip, started_at, last_seen_at)
VALUES ($1::uuid, 'web', NULLIF($2, ''), NULLIF($3, '')::inet, $4, $4) RETURNING id::text`
	sqlGet = `SELECT s.user_id::text, s.started_at, s.last_seen_at, s.ended_at IS NOT NULL,
  u.status = 'active' AND u.archived_at IS NULL,
  COALESCE(p.session_lifetime_min, 0), COALESCE(p.idle_timeout_min, 0)
FROM core.user_session s JOIN core.app_user u ON u.id = s.user_id
LEFT JOIN core.sign_in_policy p ON p.singleton
WHERE s.id = $1::uuid AND s.platform = 'web'`
	sqlTouch = `UPDATE core.user_session SET last_seen_at = $2 WHERE id = $1::uuid AND ended_at IS NULL`
	sqlEnd   = `UPDATE core.user_session SET ended_at = $2, end_reason = $3 WHERE id = $1::uuid AND ended_at IS NULL`
	sqlUser  = `SELECT id::text FROM core.app_user
WHERE lower(email) = lower($1) AND status = 'active' AND principal_kind = 'human' AND archived_at IS NULL`
)

// Start implements Store.
func (p PGStore) Start(ctx context.Context, userID, ip, device string, now time.Time) (string, error) {
	var id string
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlStart, userID, device, ip, now).Scan(&id)
	})
	if err != nil {
		return "", fmt.Errorf("start session: %w", err)
	}
	return id, nil
}

// Get implements Store.
func (p PGStore) Get(ctx context.Context, id string) (Session, error) {
	if !uuidRE.MatchString(id) {
		return Session{}, ErrNoSession
	}
	s := Session{ID: id}
	var lifeMin, idleMin int
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlGet, id).Scan(&s.UserID, &s.StartedAt, &s.LastSeenAt, &s.Ended, &s.UserActive,
			&lifeMin, &idleMin)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Session{}, ErrNoSession
	case err != nil:
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	s.Lifetime = time.Duration(lifeMin) * time.Minute
	s.Idle = time.Duration(idleMin) * time.Minute
	return s, nil
}

// Touch implements Store.
func (p PGStore) Touch(ctx context.Context, id string, now time.Time) error {
	return p.exec(ctx, "touch session", sqlTouch, id, now)
}

// End implements Store.
func (p PGStore) End(ctx context.Context, id string, now time.Time, reason string) error {
	if !uuidRE.MatchString(id) {
		return nil
	}
	return p.exec(ctx, "end session", sqlEnd, id, now, reason)
}

// ActiveByEmail implements Users.
func (p PGStore) ActiveByEmail(ctx context.Context, email string) (string, error) {
	var id string
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlUser, email).Scan(&id)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", ErrNoUser
	case err != nil:
		// The email is client data: it is never part of the error.
		return "", fmt.Errorf("user by email: %w", err)
	}
	return id, nil
}

func (p PGStore) exec(ctx context.Context, what, sql string, args ...any) error {
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	})
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}
