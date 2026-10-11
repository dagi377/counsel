package timekeeping

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// ErrNoDatabase: PGTimekeepers has no database, so it cannot list anyone.
var ErrNoDatabase = errors.New("timekeeping: timekeepers: no database")

// PGTimekeepers lists the firm's active timekeepers from core.app_user: active human users,
// not archived or expired, whose active role grants time.record at any level but none. It
// returns user ids only. It is the user source of the per-user scheduled features
// (trigger.ActiveUsers; F-TIM-01's end-of-day run).
type PGTimekeepers struct {
	DB db.DB
}

// activeTimekeepersSQL: an agent or service principal never records time, and a user whose
// account expired in the business zone (America/Toronto) no longer runs.
const activeTimekeepersSQL = `SELECT u.id::text
  FROM core.app_user u
  JOIN core.role r ON r.id = u.role_id
  JOIN core.role_grant g ON g.role_id = r.id
  WHERE u.status = 'active' AND u.principal_kind = 'human' AND u.archived_at IS NULL
    AND (u.expires_on IS NULL OR u.expires_on >= (now() AT TIME ZONE 'America/Toronto')::date)
    AND r.status = 'active' AND r.archived_at IS NULL
    AND g.capability_id = $1 AND g.level <> 'none'
  ORDER BY u.id`

// ActiveUsers implements trigger.ActiveUsers. Every per-user feature runs for the same
// timekeepers, so featureID does not narrow the list.
func (p PGTimekeepers) ActiveUsers(ctx context.Context, featureID string) ([]string, error) {
	if p.DB == nil {
		return nil, fmt.Errorf("active timekeepers for %s: %w", featureID, ErrNoDatabase)
	}
	var out []string
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// rule: LSO-3.6
		// Only users who may record time get an end-of-day run on their own time.
		rows, err := tx.Query(ctx, activeTimekeepersSQL, CapRecord)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("active timekeepers for %s: %w", featureID, err)
	}
	return out, nil
}
