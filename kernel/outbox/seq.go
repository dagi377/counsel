package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// EmitSeq writes one outbox row in tx like Emit, and numbers it with the subject's next
// subject_seq (migrations/0500_outbox_subject_seq.sql). Numbers start at 1 and have no gaps
// per (aggregate, aggregate_id): a transaction-scoped advisory lock on the subject serialises
// writers until commit, so a rolled-back emit never consumes a number. It returns the number
// it assigned.
func (p *PG) EmitSeq(ctx context.Context, tx pgx.Tx, s Subject, typ string, body any) (int64, error) {
	if typ == "" || s.Kind == "" || s.ID == "" {
		return 0, fmt.Errorf("emit seq: %w", ErrInvalidEvent)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("emit seq %s: %w: %w", typ, ErrInvalidEvent, err)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return 0, fmt.Errorf("emit seq %s: body is not a JSON object: %w", typ, ErrInvalidEvent)
	}
	// Held until tx ends; a hash collision between subjects only serialises them.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text || ':' || $2::text, 0))`,
		s.Kind, s.ID); err != nil {
		return 0, fmt.Errorf("emit seq %s: lock subject: %w", typ, err)
	}
	var seq int64
	if err := tx.QueryRow(ctx, `
INSERT INTO platform.outbox (topic, aggregate, aggregate_id, payload, subject_seq)
SELECT $1::text, $2::text, $3::uuid, $4::jsonb, COALESCE(MAX(o.subject_seq), 0) + 1
  FROM platform.outbox o
 WHERE o.aggregate = $2::text AND o.aggregate_id = $3::uuid
RETURNING subject_seq`, typ, s.Kind, s.ID, raw).Scan(&seq); err != nil {
		return 0, fmt.Errorf("emit seq %s: %w", typ, err)
	}
	return seq, nil
}
