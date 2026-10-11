package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

// Pending returns up to limit unpublished messages, oldest first.
func (p *PG) Pending(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := p.conn.Query(ctx, `
SELECT id::text, topic, aggregate, aggregate_id::text, payload, created_at, published_at, attempts
  FROM platform.outbox
 WHERE published_at IS NULL
 ORDER BY created_at, id
 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending: %w", err)
	}
	msgs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Message, error) {
		var m Message
		err := r.Scan(&m.ID, &m.Type, &m.Subject.Kind, &m.Subject.ID, &m.Payload, &m.CreatedAt, &m.PublishedAt, &m.Attempts)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("pending: %w", err)
	}
	return msgs, nil
}

// MarkPublished sets published_at once; a second call is a no-op.
func (p *PG) MarkPublished(ctx context.Context, id string) error {
	var found bool
	err := p.conn.QueryRow(ctx, `
WITH upd AS (
  UPDATE platform.outbox SET published_at = now() WHERE id = $1 AND published_at IS NULL RETURNING 1
)
SELECT EXISTS (SELECT 1 FROM platform.outbox WHERE id = $1)`, id).Scan(&found)
	if err != nil {
		return fmt.Errorf("mark published: %w", err)
	}
	if !found {
		return fmt.Errorf("mark published %s: %w", id, ErrNotFound)
	}
	return nil
}

// MarkFailed counts one more attempt and leaves the message pending.
func (p *PG) MarkFailed(ctx context.Context, id string) error {
	tag, err := p.conn.Exec(ctx, `UPDATE platform.outbox SET attempts = attempts + 1 WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("mark failed %s: %w", id, ErrNotFound)
	}
	return nil
}

// TxRunner runs fn in one transaction (kernel/db.DB satisfies it).
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// Consumer is one named handler of outbox messages.
type Consumer struct {
	Name   string
	Handle Handler
}

// Deliverer moves pending messages to consumers. Delivery is at least once: a relay that
// dies between a consumer's commit and MarkPublished delivers the message again, and the
// consumer's inbox row turns the repeat into a no-op, so each effect applies exactly once.
type Deliverer struct {
	Relay     Relay
	Inbox     Inbox
	DB        TxRunner
	Consumers []Consumer
}

// Deliver handles up to limit pending messages and returns how many it published.
func (d *Deliverer) Deliver(ctx context.Context, limit int) (int, error) {
	msgs, err := d.Relay.Pending(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("deliver: %w", err)
	}
	published := 0
	for _, m := range msgs {
		if err := d.deliverOne(ctx, m); err != nil {
			slog.WarnContext(ctx, "outbox delivery failed", "message_id", m.ID, "type", m.Type, "err", err)
			if ferr := d.Relay.MarkFailed(ctx, m.ID); ferr != nil {
				return published, fmt.Errorf("deliver %s: %w", m.ID, errors.Join(err, ferr))
			}
			continue
		}
		if err := d.Relay.MarkPublished(ctx, m.ID); err != nil {
			return published, fmt.Errorf("deliver %s: %w", m.ID, err)
		}
		published++
	}
	return published, nil
}

func (d *Deliverer) deliverOne(ctx context.Context, m Message) error {
	for _, c := range d.Consumers {
		err := d.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			first, err := d.Inbox.Claim(ctx, tx, c.Name, m.ID)
			if err != nil || !first {
				return err
			}
			return c.Handle(ctx, tx, m)
		})
		if err != nil {
			return fmt.Errorf("consumer %s: %w", c.Name, err)
		}
	}
	return nil
}
