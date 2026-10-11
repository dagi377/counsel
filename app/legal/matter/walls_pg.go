package matter

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/db"
)

// PGWallStore reads the matter module's ethical walls (matter.ethical_wall,
// ethical_wall_matter, ethical_wall_screened_user). It is the store roles.WallResolver
// checks: Screened decides visibility and Walled decides whether the check publishes
// roles.walled_matter_accessed. Only active walls count.
type PGWallStore struct {
	DB db.DB
}

var (
	_ roles.WallStore   = PGWallStore{}
	_ roles.WalledStore = PGWallStore{}
)

const (
	sqlWallScreened = `SELECT EXISTS (SELECT 1 FROM matter.ethical_wall_matter wm
  JOIN matter.ethical_wall w ON w.id = wm.wall_id AND w.status = 'active'
  JOIN matter.ethical_wall_screened_user su ON su.wall_id = w.id
  WHERE wm.matter_id = $1::uuid AND su.user_id = $2::uuid)`
	sqlWallWalled = `SELECT EXISTS (SELECT 1 FROM matter.ethical_wall_matter wm
  JOIN matter.ethical_wall w ON w.id = wm.wall_id AND w.status = 'active'
  WHERE wm.matter_id = $1::uuid)`
)

// Screened implements roles.WallStore: an active wall on matterID screens userID.
func (s PGWallStore) Screened(ctx context.Context, userID, matterID string) (bool, error) {
	return s.exists(ctx, "screened", sqlWallScreened, matterID, userID)
}

// Walled implements roles.WalledStore: an active wall covers matterID.
func (s PGWallStore) Walled(ctx context.Context, matterID string) (bool, error) {
	return s.exists(ctx, "walled", sqlWallWalled, matterID)
}

func (s PGWallStore) exists(ctx context.Context, op, sql string, args ...any) (bool, error) {
	if s.DB == nil {
		return false, fmt.Errorf("walls %s: no database: %w", op, ErrInvalid)
	}
	var out bool
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sql, args...).Scan(&out)
	})
	if err != nil {
		return false, fmt.Errorf("walls %s: %w", op, err)
	}
	return out, nil
}
