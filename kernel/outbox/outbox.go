package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Conn is the part of a pgx pool or connection the relay side uses.
type Conn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PG is the Postgres outbox over platform.outbox and platform.inbox. Emit and Claim run in
// the caller's tx; the Relay methods run on conn.
type PG struct {
	conn Conn
}

var (
	_ Emitter = (*PG)(nil)
	_ Relay   = (*PG)(nil)
	_ Inbox   = (*PG)(nil)
)

// NewPG returns an outbox on conn.
func NewPG(conn Conn) *PG { return &PG{conn: conn} }

// Emit writes one outbox row in tx, so the event commits or rolls back with the change.
// body must encode as a JSON object of ids, states and amounts.
func (p *PG) Emit(ctx context.Context, tx pgx.Tx, s Subject, typ string, body any) error {
	if typ == "" || s.Kind == "" || s.ID == "" {
		return fmt.Errorf("emit: %w", ErrInvalidEvent)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("emit %s: %w: %w", typ, ErrInvalidEvent, err)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return fmt.Errorf("emit %s: body is not a JSON object: %w", typ, ErrInvalidEvent)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.outbox (topic, aggregate, aggregate_id, payload) VALUES ($1, $2, $3, $4)`,
		typ, s.Kind, s.ID, raw); err != nil {
		return fmt.Errorf("emit %s: %w", typ, err)
	}
	return nil
}

// Claim records in tx that consumer handles messageID (platform.inbox). It returns false for
// a message already processed, and the caller then skips its effect. It is the one inbox
// claim; kernel/idem has none.
// rule: ARC-U-022
func (p *PG) Claim(ctx context.Context, tx pgx.Tx, consumer, messageID string) (bool, error) {
	// rule: ARC-I-030
	tag, err := tx.Exec(ctx, `
INSERT INTO platform.inbox (consumer, message_id) VALUES ($1, $2)
ON CONFLICT (consumer, message_id) DO NOTHING`, consumer, messageID)
	if err != nil {
		return false, fmt.Errorf("inbox claim %s: %w", consumer, err)
	}
	return tag.RowsAffected() == 1, nil
}
