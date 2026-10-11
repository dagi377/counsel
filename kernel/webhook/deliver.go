package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// ErrOutOfOrder means a delivery was acked while an older delivery of the same endpoint and
// subject is still unacked. Only the head of a subject's queue may be acked.
var ErrOutOfOrder = errors.New("webhook: delivery is not the head of its subject queue")

// PayloadSource loads message payloads by the id of the outbox row that holds them. A
// delivery row keeps only that pointer (payload_ref), never the payload.
type PayloadSource interface {
	Payloads(ctx context.Context, tx pgx.Tx, ids []string) (map[string]json.RawMessage, error)
}

// OutboxPayloads reads payloads from platform.outbox by row id. It reads only the rows the
// deliveries point at and writes nothing.
type OutboxPayloads struct{}

// Payloads returns the payload of each outbox row in ids; a missing row is absent from the map.
func (OutboxPayloads) Payloads(ctx context.Context, tx pgx.Tx, ids []string) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT id::text, payload FROM platform.outbox WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("outbox payloads: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var p []byte
		if err := rows.Scan(&id, &p); err != nil {
			return nil, fmt.Errorf("outbox payloads: scan: %w", err)
		}
		out[id] = json.RawMessage(p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("outbox payloads: %w", err)
	}
	return out, nil
}

// subjectKey is the webhook_delivery.subject text of s: "<kind>:<id>".
func subjectKey(s Subject) (string, error) {
	if s.Kind == "" || s.ID == "" || strings.Contains(s.Kind, ":") {
		return "", fmt.Errorf("subject: %w", ErrInvalidPayload)
	}
	return s.Kind + ":" + s.ID, nil
}

// parseSubject is the inverse of subjectKey.
func parseSubject(key string) Subject {
	kind, id, _ := strings.Cut(key, ":")
	return Subject{Kind: kind, ID: id}
}

// Queue delivers webhook messages in order per (endpoint, subject) on
// platform.webhook_delivery: one row per message and endpoint, retried in place, released
// to the next message of its subject only once acked. Endpoints are the active
// platform.webhook rows; a row with no events subscribes to every type.
type Queue struct {
	db       db.DB
	types    *Types
	payloads PayloadSource
}

// NewQueue returns a queue on pool. types decides which messages may be emitted; payloads
// loads message bodies when deliveries are handed out.
func NewQueue(pool db.DB, types *Types, payloads PayloadSource) *Queue {
	return &Queue{db: pool, types: types, payloads: payloads}
}

// Enqueue queues m for every active endpoint subscribed to m.Type, inside tx. m.ID is the
// id of the outbox row holding m.Payload: it becomes both the stable webhook-id and the
// payload_ref. Enqueueing the same message twice queues it once per endpoint.
func (q *Queue) Enqueue(ctx context.Context, tx pgx.Tx, m Message) ([]DeliveryID, error) {
	if err := q.types.Check(m); err != nil {
		return nil, fmt.Errorf("enqueue: %w", err)
	}
	subject, err := subjectKey(m.Subject)
	if err != nil {
		return nil, fmt.Errorf("enqueue %s: %w", m.Type, err)
	}
	if m.ID == "" {
		return nil, fmt.Errorf("enqueue %s: message id: %w", m.Type, ErrInvalidPayload)
	}
	rows, err := tx.Query(ctx,
		`INSERT INTO platform.webhook_delivery (webhook_id, event, subject, message_id, payload_ref)
		 SELECT w.id, $1, $2, $3::uuid, $3::uuid
		   FROM platform.webhook w
		  WHERE w.status = 'active' AND (cardinality(w.events) = 0 OR $1 = ANY(w.events))
		    AND NOT EXISTS (SELECT 1 FROM platform.webhook_delivery d
		                     WHERE d.webhook_id = w.id AND d.message_id = $3::uuid)
		 ON CONFLICT (webhook_id, message_id, attempt) WHERE message_id IS NOT NULL DO NOTHING
		 RETURNING id::text`,
		m.Type, subject, m.ID)
	if err != nil {
		return nil, fmt.Errorf("enqueue %s: %w", m.Type, err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("enqueue %s: %w", m.Type, err)
	}
	out := make([]DeliveryID, len(ids))
	for i, id := range ids {
		out[i] = DeliveryID(id)
	}
	return out, nil
}

// Ready returns the deliveries for endpoint that may be sent now: for each subject, only
// its oldest unacked delivery. Message 2 of a subject is never returned while message 1 is
// unacked.
func (q *Queue) Ready(ctx context.Context, endpoint string) ([]Delivery, error) {
	var out []Delivery
	err := q.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM platform.webhook WHERE id::text = $1)`, endpoint).Scan(&exists); err != nil {
			return fmt.Errorf("endpoint: %w", err)
		}
		if !exists {
			return ErrUnknownEndpoint
		}
		rows, err := tx.Query(ctx,
			`SELECT DISTINCT ON (subject) id::text, event, subject, message_id::text,
			        payload_ref::text, attempt, at
			   FROM platform.webhook_delivery
			  WHERE webhook_id::text = $1 AND acked_at IS NULL AND subject IS NOT NULL
			  ORDER BY subject, at, id`, endpoint)
		if err != nil {
			return fmt.Errorf("query: %w", err)
		}
		type head struct {
			d   Delivery
			ref string
		}
		var heads []head
		for rows.Next() {
			var h head
			var subject string
			var attempt int
			if err := rows.Scan(&h.d.ID, &h.d.Message.Type, &subject, &h.d.Message.ID, &h.ref,
				&attempt, &h.d.Message.Timestamp); err != nil {
				rows.Close()
				return fmt.Errorf("scan: %w", err)
			}
			h.d.Endpoint = endpoint
			h.d.Message.Subject = parseSubject(subject)
			h.d.Attempts = attempt - 1
			heads = append(heads, h)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("rows: %w", err)
		}
		refs := make([]string, 0, len(heads))
		for _, h := range heads {
			refs = append(refs, h.ref)
		}
		payloads, err := q.payloads.Payloads(ctx, tx, refs)
		if err != nil {
			return fmt.Errorf("payloads: %w", err)
		}
		for _, h := range heads {
			p, ok := payloads[h.ref]
			if !ok {
				return fmt.Errorf("delivery %s: payload: %w", h.d.ID, ErrNotFound)
			}
			h.d.Message.Payload = p
			out = append(out, h.d)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ready %s: %w", endpoint, err)
	}
	return out, nil
}

// Ack marks a delivery sent, releasing the next delivery of its subject. Acking an acked
// delivery is a no-op; acking one behind an unacked delivery returns ErrOutOfOrder.
func (q *Queue) Ack(ctx context.Context, id DeliveryID) error {
	err := q.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var acked, behind bool
		err := tx.QueryRow(ctx,
			`SELECT d.acked_at IS NOT NULL,
			        EXISTS (SELECT 1 FROM platform.webhook_delivery o
			                 WHERE o.webhook_id = d.webhook_id AND o.subject = d.subject
			                   AND o.acked_at IS NULL AND (o.at, o.id) < (d.at, d.id))
			   FROM platform.webhook_delivery d
			  WHERE d.id::text = $1
			    FOR UPDATE OF d`, string(id)).Scan(&acked, &behind)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		if acked {
			return nil
		}
		if behind {
			return ErrOutOfOrder
		}
		if _, err := tx.Exec(ctx,
			`UPDATE platform.webhook_delivery SET acked_at = now() WHERE id::text = $1`, string(id)); err != nil {
			return fmt.Errorf("update: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("ack %s: %w", id, err)
	}
	return nil
}

// Fail records a failed attempt; an unknown or already acked delivery returns ErrNotFound.
// The delivery stays unacked at the head of its subject's
// queue, so later messages of that subject keep waiting.
func (q *Queue) Fail(ctx context.Context, id DeliveryID) error {
	err := q.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE platform.webhook_delivery SET attempt = attempt + 1
			  WHERE id::text = $1 AND acked_at IS NULL`, string(id))
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("fail %s: %w", id, err)
	}
	return nil
}
