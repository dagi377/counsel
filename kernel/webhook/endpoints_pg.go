package webhook

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// PGEndpoints lists the endpoints the delivery worker drains (kernel/boot.WebhookEndpoints).
type PGEndpoints struct {
	DB db.DB
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
}

// sqlActiveEndpoints selects the ids of active endpoints that have not expired. It reads
// only the id, never secret_ref.
const sqlActiveEndpoints = `SELECT id::text FROM platform.webhook
WHERE status = 'active' AND (expires_at IS NULL OR expires_at > $1)`

// Endpoints returns the ids of active, unexpired endpoints, sorted.
func (e PGEndpoints) Endpoints(ctx context.Context) ([]string, error) {
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	var ids []string
	err := e.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlActiveEndpoints, now())
		if err != nil {
			return fmt.Errorf("query: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("rows: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("webhook endpoints: %w", err)
	}
	sort.Strings(ids)
	return ids, nil
}
