package matter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// The matter and its retention record are locked, so a hold placed concurrently waits for
	// the caller's tx or is seen by it.
	sqlRetention = `SELECT m.status, m.legal_hold, c.legal_hold, c.retention_class, c.destroy_after,
  c.destroyed_at IS NOT NULL,
  EXISTS (SELECT 1 FROM docs.document d WHERE d.matter_id = m.id AND d.legal_hold),
  EXISTS (SELECT 1 FROM matter.matter o WHERE o.client_id = m.client_id AND o.id <> m.id
    AND o.status NOT IN ('closed','archived')),
  c.keep_set
FROM matter.matter m JOIN matter.matter_closing c ON c.matter_id = m.id
WHERE m.id = $1::uuid
FOR UPDATE OF m, c`
	sqlRecordDestroyed = `UPDATE matter.matter_closing
SET destroyed_at = $2, destruction_certificate_ref = 'destruction_order:' || $3::text
WHERE matter_id = $1::uuid AND destroyed_at IS NULL AND NOT legal_hold`
)

// Retention is a closed matter's retention state as a destruction run sees it.
type Retention struct {
	Found            bool // the matter has a retention record
	Status           string
	MatterHold       bool // matter.legal_hold
	RecordHold       bool // matter_closing.legal_hold
	DocumentHold     bool // some document of the matter is on legal hold
	RelatedOpen      bool // another matter of the same client is not closed
	RetentionClass   string
	DestroyAfter     *time.Time
	AlreadyDestroyed bool
	KeepSet          []string
}

// PGRetention reads and stamps matter.matter_closing inside the caller's tx.
type PGRetention struct{}

// Retention locks the matter and its retention record and reads them. A matter without a
// retention record is not Found.
func (PGRetention) Retention(ctx context.Context, tx pgx.Tx, matterID string) (Retention, error) {
	var (
		r    Retention
		keep []byte
	)
	err := tx.QueryRow(ctx, sqlRetention, matterID).Scan(&r.Status, &r.MatterHold, &r.RecordHold, &r.RetentionClass,
		&r.DestroyAfter, &r.AlreadyDestroyed, &r.DocumentHold, &r.RelatedOpen, &keep)
	if errors.Is(err, pgx.ErrNoRows) {
		return Retention{}, nil
	}
	if err != nil {
		return Retention{}, fmt.Errorf("retention of matter %s: %w", matterID, err)
	}
	r.Found = true
	if len(keep) > 0 {
		var raw []any
		if err := json.Unmarshal(keep, &raw); err != nil {
			return Retention{}, fmt.Errorf("retention of matter %s: keep set: %w", matterID, err)
		}
		for _, v := range raw {
			if s, ok := v.(string); ok {
				r.KeepSet = append(r.KeepSet, s)
			}
		}
	}
	return r, nil
}

// RecordDestroyed stamps the retention record destroyed under gateID; a record on hold or
// already destroyed is an error.
func (PGRetention) RecordDestroyed(ctx context.Context, tx pgx.Tx, matterID, gateID string, at time.Time) error {
	tag, err := tx.Exec(ctx, sqlRecordDestroyed, matterID, at, gateID)
	if err != nil {
		return fmt.Errorf("record destroyed %s: %w", matterID, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("record destroyed %s: record on hold or already destroyed", matterID)
	}
	return nil
}
