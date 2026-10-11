package sync

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
)

// ErrSubscriptionNotFound is returned when no platform.source_subscription row has the id.
var ErrSubscriptionNotFound = errors.New("clio/sync: subscription not found")

// MaxLastErrorLen is the most runes of a renewal error stored in last_error.
const MaxLastErrorLen = 500

var _ SubscriptionStore = (*PGSubscriptions)(nil)

// PGSubscriptions is the Postgres SubscriptionStore over platform.source_subscription. It
// stores ids, events and times; a renewal error is redacted and truncated before it is
// stored, so no token or secret value reaches the row.
type PGSubscriptions struct {
	db db.DB
}

// NewPGSubscriptions returns a PGSubscriptions on d.
func NewPGSubscriptions(d db.DB) *PGSubscriptions { return &PGSubscriptions{db: d} }

const expiringSQL = `SELECT id::text, source, remote_id, events, expires_at, last_renewed_at, COALESCE(last_error, '')
FROM platform.source_subscription
WHERE expires_at <= $1
ORDER BY expires_at, id`

const markRenewedSQL = `UPDATE platform.source_subscription
SET expires_at = $2, last_renewed_at = $3, last_error = NULL
WHERE id = $1::uuid`

const markFailedSQL = `UPDATE platform.source_subscription
SET last_error = $2
WHERE id = $1::uuid`

// Expiring returns the rows with expires_at <= before, soonest first.
func (s *PGSubscriptions) Expiring(ctx context.Context, before time.Time) ([]Subscription, error) {
	if before.IsZero() {
		return nil, errors.New("expiring subscriptions: no cut-off time")
	}
	var out []Subscription
	err := s.inTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, expiringSQL, before)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sub Subscription
			if err := rows.Scan(&sub.ID, &sub.Source, &sub.RemoteID, &sub.Events, &sub.ExpiresAt, &sub.LastRenewedAt, &sub.LastError); err != nil {
				return err
			}
			out = append(out, sub)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("expiring subscriptions: %w", err)
	}
	return out, nil
}

// MarkRenewed sets expires_at and last_renewed_at and clears last_error.
func (s *PGSubscriptions) MarkRenewed(ctx context.Context, id string, expiresAt, renewedAt time.Time) error {
	if id == "" || expiresAt.IsZero() || renewedAt.IsZero() {
		return errors.New("mark subscription renewed: missing id or time")
	}
	return s.update(ctx, "mark subscription renewed", markRenewedSQL, id, expiresAt, renewedAt)
}

// MarkFailed sets last_error, redacted and truncated, and leaves expires_at unchanged. An
// empty error is still recorded as a failure.
func (s *PGSubscriptions) MarkFailed(ctx context.Context, id, lastError string) error {
	if id == "" {
		return errors.New("mark subscription failed: missing id")
	}
	return s.update(ctx, "mark subscription failed", markFailedSQL, id, RedactLastError(lastError))
}

// update runs one single-row UPDATE and fails unless exactly one row changed.
func (s *PGSubscriptions) update(ctx context.Context, op, sql string, args ...any) error {
	err := s.inTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, args...)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
				return ErrSubscriptionNotFound
			}
			return err
		}
		switch n := tag.RowsAffected(); n {
		case 1:
			return nil
		case 0:
			return ErrSubscriptionNotFound
		default:
			return fmt.Errorf("%d rows changed, want 1", n)
		}
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func (s *PGSubscriptions) inTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	if s == nil || s.db == nil {
		return errors.New("no database")
	}
	return s.db.InTx(ctx, fn)
}

// secretPatterns match credential values in an error message. Each keeps its label (group
// 1) and replaces the value.
var secretPatterns = []*regexp.Regexp{
	// Authorization headers: "Authorization: Bearer abc", "bearer abc", "Basic abc".
	regexp.MustCompile(`(?i)((?:authorization)\s*[:=]\s*(?:bearer|basic|token)?\s*)[^\s,;"']+`),
	regexp.MustCompile(`(?i)(\b(?:bearer|basic)\s+)[^\s,;"']+`),
	// key=value and "key": "value" pairs for anything that names a secret.
	regexp.MustCompile(`(?i)((?:access_token|refresh_token|id_token|token|client_secret|secret|password|passwd|api_key|apikey|signature)"?\s*[:=]\s*"?)[^\s&,;"']+`),
	// URL user-info: scheme://user:pass@host.
	regexp.MustCompile(`(?i)(://[^/\s:@]+:)[^@\s/]+(@)`),
}

// opaquePattern matches long opaque strings (tokens, JWTs) that carry no label.
var opaquePattern = regexp.MustCompile(`[A-Za-z0-9_\-]{24,}(?:\.[A-Za-z0-9_\-]{8,}){0,2}`)

// RedactLastError removes credential values from a renewal error, then truncates it to
// MaxLastErrorLen runes on a rune boundary. An empty error becomes "renewal failed", so a
// failure is never recorded as no error.
func RedactLastError(msg string) string {
	msg = strings.ToValidUTF8(msg, "?")
	for _, p := range secretPatterns {
		if p.NumSubexp() == 2 {
			msg = p.ReplaceAllString(msg, "${1}[redacted]${2}")
			continue
		}
		msg = p.ReplaceAllString(msg, "${1}[redacted]")
	}
	msg = opaquePattern.ReplaceAllString(msg, "[redacted]")
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "renewal failed"
	}
	if utf8.RuneCountInString(msg) > MaxLastErrorLen {
		r := []rune(msg)
		msg = string(r[:MaxLastErrorLen-1]) + "…"
	}
	return msg
}
