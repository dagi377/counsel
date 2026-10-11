package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/db"
)

// ErrNotDesignatable: the user named as a feature's designated lawyer is not an active human
// lawyer in good standing, or the feature id is not a feature id.
var ErrNotDesignatable = errors.New("agent: user cannot be a designated lawyer")

var featureIDPattern = regexp.MustCompile(`^F-[A-Z]+-[0-9]+[a-z]?$`)

// PGDesignatedLawyers keeps the firm setting that names, per scheduled AI feature, the lawyer
// its runs act for (agent.designated_lawyer). It holds ids only.
type PGDesignatedLawyers struct {
	db db.DB
}

var _ trigger.DesignatedLawyers = (*PGDesignatedLawyers)(nil)

// NewPGDesignatedLawyers returns a PGDesignatedLawyers on pool.
func NewPGDesignatedLawyers(pool db.DB) *PGDesignatedLawyers {
	return &PGDesignatedLawyers{db: pool}
}

const (
	designatedSQL = `SELECT d.user_id::text FROM agent.designated_lawyer d WHERE d.feature_id = $1`
	// A designated lawyer must still be an active human lawyer in good standing when a run
	// starts; anyone else leaves the feature unready.
	designatableSQL = `SELECT EXISTS (SELECT 1 FROM core.app_user u WHERE u.id = $1::uuid
  AND u.status = 'active' AND u.principal_kind = 'human'
  AND u.licence_type = 'lawyer' AND u.licence_status = 'good_standing')`
	setDesignatedSQL = `INSERT INTO agent.designated_lawyer (feature_id, user_id, updated_by)
VALUES ($1, $2::uuid, $3::uuid)
ON CONFLICT (feature_id) DO UPDATE SET user_id = EXCLUDED.user_id, updated_by = EXCLUDED.updated_by,
  updated_at = now(), row_version = agent.designated_lawyer.row_version + 1`
)

// DesignatedLawyer implements trigger.DesignatedLawyers. It returns "" when the firm named
// nobody for featureID, or when the named user is no longer an active lawyer in good
// standing, so the run fails closed.
func (p *PGDesignatedLawyers) DesignatedLawyer(ctx context.Context, featureID string) (string, error) {
	var user string
	err := p.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, designatedSQL, featureID).Scan(&user); err != nil {
			return err
		}
		ok, err := designatable(ctx, tx, user)
		if err != nil {
			return err
		}
		if !ok {
			user = ""
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("designated lawyer %s: %w", featureID, err)
	}
	return user, nil
}

// Set names userID as featureID's designated lawyer, recorded as set by by. The user must be
// an active human lawyer in good standing.
func (p *PGDesignatedLawyers) Set(ctx context.Context, featureID, userID, by string) error {
	if !featureIDPattern.MatchString(featureID) || userID == "" || by == "" {
		return fmt.Errorf("set designated lawyer %s: feature, user and setter required: %w", featureID, ErrNotDesignatable)
	}
	err := p.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		ok, err := designatable(ctx, tx, userID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotDesignatable
		}
		_, err = tx.Exec(ctx, setDesignatedSQL, featureID, userID, by)
		return err
	})
	if err != nil {
		return fmt.Errorf("set designated lawyer %s: %w", featureID, err)
	}
	return nil
}

func designatable(ctx context.Context, tx pgx.Tx, userID string) (bool, error) {
	var ok bool
	if err := tx.QueryRow(ctx, designatableSQL, userID).Scan(&ok); err != nil {
		return false, err
	}
	return ok, nil
}
