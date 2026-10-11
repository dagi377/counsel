package matter

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// ErrWallsGateOwned is returned by PGWalls.AddWall: a wall is created only by the
// conflict.wall gate's effect, never through the matter write path.
var ErrWallsGateOwned = errors.New("matter: walls change only through their gate")

// PGUpdater is the Postgres Updater: it writes the plain matter fields of one matter.matter
// row at an expected row version, in a transaction bound to the acting principal so the
// matter RLS policies (walls and visibility) apply to the write.
type PGUpdater struct {
	db db.DB
}

var _ Updater = (*PGUpdater)(nil)

// NewPGUpdater returns a PGUpdater on pool.
func NewPGUpdater(pool db.DB) *PGUpdater { return &PGUpdater{db: pool} }

const (
	// lockForUpdateSQL reads the row version and whether user $2 is screened from the matter
	// by an active wall, locking the row for the update that follows.
	lockForUpdateSQL = `SELECT m.row_version,
       EXISTS (SELECT 1 FROM matter.ethical_wall_matter wm
                 JOIN matter.ethical_wall w ON w.id = wm.wall_id AND w.status = 'active'
                 JOIN matter.ethical_wall_screened_user su ON su.wall_id = w.id
                WHERE wm.matter_id = m.id AND su.user_id::text = $2)
  FROM matter.matter m WHERE m.id = $1::uuid FOR UPDATE OF m`
	// updatePlainSQL writes the plain fields; a NULL argument keeps the column, and "" clears
	// a province. core.touch bumps row_version and sets updated_by.
	updatePlainSQL = `UPDATE matter.matter m SET
  short_name        = coalesce($3::text, m.short_name),
  practice_area_id  = coalesce($4::uuid, m.practice_area_id),
  court_province    = CASE WHEN $5::text IS NULL THEN m.court_province ELSE nullif($5::text, '') END,
  property_province = CASE WHEN $6::text IS NULL THEN m.property_province ELSE nullif($6::text, '') END
 WHERE m.id = $1::uuid AND m.row_version = $2
RETURNING ` + matterColumns
	wallExistsSQL = `SELECT EXISTS (SELECT 1 FROM matter.ethical_wall_matter wm
                 JOIN matter.ethical_wall w ON w.id = wm.wall_id AND w.status = 'active'
                 JOIN matter.ethical_wall_screened_user su ON su.wall_id = w.id
                WHERE wm.matter_id::text = $1 AND su.user_id::text = $2)`
)

// validChange reports whether c is a non-empty plain change with valid values.
//
// rule: BIL-T-011
// rule: BIL-T-012
func validChange(c PlainChange) bool {
	if c.ShortName == nil && c.PracticeAreaID == nil && c.CourtProvince == nil && c.PropertyProvince == nil {
		return false
	}
	if (c.ShortName != nil && *c.ShortName == "") || (c.PracticeAreaID != nil && *c.PracticeAreaID == "") {
		return false
	}
	return validProvince(c.CourtProvince) && validProvince(c.PropertyProvince)
}

// updateSession is the db session a write by p runs under: an agent writes as the user it
// acts for, so the walls of that user apply.
func updateSession(p registry.Principal) db.Session {
	s := db.Session{UserID: principalID(p), PrincipalKind: string(p.Kind)}
	if p.Kind == registry.PrincipalAgent {
		s.RunID, s.ActingFor = p.RunID, p.ActingFor
	}
	return s
}

// checkLocked decides a locked row: a walled actor sees no matter (ARC-I-042), and a
// version other than the expected one is a stale If-Match (ARC-I-006).
func checkLocked(id ID, current, expected int, walled bool) error {
	// rule: ARC-I-042
	if walled {
		return fmt.Errorf("matter %s: %w", id, ErrNotFound)
	}
	// rule: ARC-I-006
	if current != expected {
		return fmt.Errorf("matter %s: version %d, want %d: %w", id, current, expected, server.ErrPreconditionFailed)
	}
	return nil
}

// Update implements Updater. A matter the principal cannot see (missing, walled or hidden
// by RLS) is ErrNotFound; a stale version is server.ErrPreconditionFailed; an invalid change
// or an unknown practice area is ErrInvalid.
func (u *PGUpdater) Update(ctx context.Context, id ID, version int, c PlainChange, by registry.Principal) (Matter, error) {
	if id == "" || !validChange(c) {
		return Matter{}, fmt.Errorf("update matter: %w", ErrInvalid)
	}
	actor := principalID(by)
	if actor == "" {
		return Matter{}, fmt.Errorf("update matter %s: no acting user: %w", id, ErrInvalid)
	}
	var out Matter
	err := u.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := u.db.Bind(ctx, tx, updateSession(by)); err != nil {
			return fmt.Errorf("bind: %w", err)
		}
		var (
			current int
			walled  bool
		)
		err := tx.QueryRow(ctx, lockForUpdateSQL, string(id), actor).Scan(&current, &walled)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("matter %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("lock: %w", pgErr(err))
		}
		if err := checkLocked(id, current, version, walled); err != nil {
			return err
		}
		m, err := scanMatter(tx.QueryRow(ctx, updatePlainSQL, string(id), version,
			c.ShortName, c.PracticeAreaID, c.CourtProvince, c.PropertyProvince))
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("matter %s: %w", id, server.ErrPreconditionFailed)
		}
		if err != nil {
			return fmt.Errorf("update: %w", pgErr(err))
		}
		out = m
		return nil
	})
	if err != nil {
		return Matter{}, fmt.Errorf("update matter: %w", err)
	}
	return out, nil
}

// pgErr maps a bad uuid or a missing practice area to ErrInvalid.
func pgErr(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "22P02" || pg.Code == "23503" || pg.Code == "23514") {
		return fmt.Errorf("%s: %w", pg.Code, ErrInvalid)
	}
	return err
}

// PGWalls answers conflict.Walls.Walled from the active ethical walls in matter.ethical_wall.
// It only reads; walls are created by their gate's effect.
type PGWalls struct {
	db db.DB
}

var _ conflict.Walls = (*PGWalls)(nil)

// NewPGWalls returns a PGWalls on pool.
func NewPGWalls(pool db.DB) *PGWalls { return &PGWalls{db: pool} }

// Walled implements conflict.Walls.
//
// rule: ARC-I-042
func (w *PGWalls) Walled(ctx context.Context, userID, matterID string) (bool, error) {
	if userID == "" || matterID == "" {
		return false, fmt.Errorf("walled: %w", ErrInvalid)
	}
	var walled bool
	err := w.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, wallExistsSQL, matterID, userID).Scan(&walled)
	})
	if err != nil {
		return false, fmt.Errorf("walled: %w", err)
	}
	return walled, nil
}

// AddWall implements conflict.Walls. It always fails: the write path never creates walls.
func (w *PGWalls) AddWall(context.Context, conflict.Wall) error {
	return fmt.Errorf("add wall: %w", ErrWallsGateOwned)
}
