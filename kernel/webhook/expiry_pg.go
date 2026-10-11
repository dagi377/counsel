package webhook

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// PGExpiry is the Postgres ExpiryStore over platform.webhook.
type PGExpiry struct {
	DB db.DB
}

var _ ExpiryStore = PGExpiry{}

// Statements PGExpiry issues.
const (
	sqlExpiring = `SELECT id::text, expires_at FROM platform.webhook
WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at <= $1
ORDER BY expires_at, id`
	// sqlClaimNotice claims the notice due at $3 (expires_at minus the lead) for the expiry
	// $2. expiry_noticed_for keeps the latest claimed notice instant, so each lead of one
	// expiry is claimed once, and a longer lead is never claimed after a shorter one.
	sqlClaimNotice = `UPDATE platform.webhook SET expiry_noticed_for = $3
WHERE id::text = $1 AND expires_at = $2
  AND (expiry_noticed_for IS NULL OR expiry_noticed_for < $3)
RETURNING id::text`
	// sqlSetExpiry sets a new expiry and resets the notice claim for it.
	sqlSetExpiry = `UPDATE platform.webhook SET expires_at = $2, expiry_noticed_for = NULL, row_version = row_version + 1
WHERE id::text = $1`
)

// Expiring returns active endpoints whose expires_at is set and is on or before before,
// soonest first. Tenant-owned (NULL expiry), disabled and expired endpoints are left out.
func (s PGExpiry) Expiring(ctx context.Context, before time.Time) ([]EndpointExpiry, error) {
	var out []EndpointExpiry
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlExpiring, before)
		if err != nil {
			return fmt.Errorf("query: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var at time.Time
			if err := rows.Scan(&id, &at); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			out = append(out, EndpointExpiry{ID: id, ExpiresAt: &at})
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("rows: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("expiring endpoints: %w", err)
	}
	return out, nil
}

// ClaimNotice records that the lead notice for (id, expiresAt) is sent, in one UPDATE.
// first is true only for the call that claimed it. An endpoint whose expiry changed since
// expiresAt was read, or that does not exist, claims nothing.
func (s PGExpiry) ClaimNotice(ctx context.Context, id string, expiresAt time.Time, lead time.Duration) (bool, error) {
	if lead <= 0 {
		return false, fmt.Errorf("claim notice %s: lead %v must be positive", id, lead)
	}
	at := expiresAt.Add(-lead)
	first := false
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var got string
		err := tx.QueryRow(ctx, sqlClaimNotice, id, expiresAt, at).Scan(&got)
		switch {
		case err == nil:
			first = true
			return nil
		case errors.Is(err, pgx.ErrNoRows):
			return nil
		default:
			return err
		}
	})
	if err != nil {
		return false, fmt.Errorf("claim notice %s: %w", id, err)
	}
	return first, nil
}

// SetExpiry sets expires_at (nil: never expires) and resets the notice claim, so the new
// expiry gets its own notices. An unknown id is ErrUnknownEndpoint.
func (s PGExpiry) SetExpiry(ctx context.Context, id string, expiresAt *time.Time) error {
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sqlSetExpiry, id, expiresAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrUnknownEndpoint
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("set expiry %s: %w", id, err)
	}
	return nil
}
