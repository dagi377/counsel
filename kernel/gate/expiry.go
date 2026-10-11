package gate

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// rule: ARC-I-022
// Expired reports whether g is past its TTL at now (expires_at = opened_at + kind TTL). A
// gate is expired from its expires_at instant on; an expired gate never runs its effect
// and is reopened as a new gate. A gate with no expiry time is treated as expired, so a
// row missing its TTL fails closed.
func Expired(g Gate, now time.Time) bool {
	return g.ExpiresAt.IsZero() || !now.Before(g.ExpiresAt)
}

// ExpireDue marks every pending gate past its expires_at as expired, in tx, and returns
// how many it closed. The River job gate.expire calls it every minute (RV2-003).
func (e *PG) ExpireDue(ctx context.Context, tx pgx.Tx) (int, error) {
	// rule: ARC-I-022
	var n int
	if err := tx.QueryRow(ctx, `SELECT gate.expire_due()`).Scan(&n); err != nil {
		return 0, fmt.Errorf("expire due gates: %w", err)
	}
	return n, nil
}
