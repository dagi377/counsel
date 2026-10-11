package deadline

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	_ Lister  = (*PGBook)(nil)
	_ SentLog = PGSentLog{}
)

const listSQL = `SELECT d.id::text, d.subject_kind, d.subject_id::text, d.pack_code, d.pack_version, d.rule_key,
       d.trigger_on, d.due_on, d.status,
       (SELECT c.user_id::text FROM cal.deadline_confirmation c
         WHERE c.deadline_id = d.id AND c.kind = 'confirm' ORDER BY c.at, c.id LIMIT 1)
  FROM cal.deadline d
 WHERE ($1::text IS NULL OR (d.subject_kind = $1 AND d.subject_id::text = $2))
   AND ($3::date IS NULL OR d.due_on >= $3)
   AND ($4::date IS NULL OR d.due_on <= $4)
 ORDER BY d.due_on, d.id`

// List returns the stored deadlines matching f, by due date then id, in its own
// transaction. It reads ids, dates and the state only.
func (b *PGBook) List(ctx context.Context, f Filter) ([]Deadline, error) {
	var kind, subject *string
	if f.Subject != nil {
		kind, subject = &f.Subject.Kind, &f.Subject.ID
	}
	var from, to *time.Time
	if f.From != nil {
		t := dateArg(*f.From)
		from = &t
	}
	if f.To != nil {
		t := dateArg(*f.To)
		to = &t
	}
	var out []Deadline
	err := b.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, listSQL, kind, subject, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, status string
			var k, s, pack, version, rule, by *string
			var trigger *time.Time
			var due time.Time
			if err := rows.Scan(&id, &k, &s, &pack, &version, &rule, &trigger, &due, &status, &by); err != nil {
				return err
			}
			d := Deadline{ID: DeadlineID(id), State: State(status), ConfirmedBy: deref(by), Proposed: Proposed{
				PackID: deref(pack), PackVersion: deref(version), RuleID: deref(rule), Due: dateOf(due),
				Subject: Subject{Kind: deref(k), ID: deref(s)},
			}}
			if trigger != nil {
				d.Proposed.Trigger = dateOf(*trigger)
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list deadlines: %w", err)
	}
	return out, nil
}

// PGSentLog is the durable SentLog over cal.deadline_reminder_sent, unique per (kind,
// deadline, user, day): a restart never sends the same reminder twice in a day.
type PGSentLog struct {
	DB TxRunner
}

const (
	sentSQL = `SELECT EXISTS (SELECT 1 FROM cal.deadline_reminder_sent
 WHERE kind = $1 AND deadline_id IS NOT DISTINCT FROM $2::uuid
   AND user_id IS NOT DISTINCT FROM $3::uuid AND day = $4)`
	markSQL = `INSERT INTO cal.deadline_reminder_sent (kind, deadline_id, user_id, day)
VALUES ($1, $2::uuid, $3::uuid, $4)
ON CONFLICT (kind, deadline_id, user_id, day) DO NOTHING`
)

// sentArgs are key's statement arguments; an empty deadline or user is NULL.
func sentArgs(key SentKey) []any {
	var dl, user *string
	if key.DeadlineID != "" {
		s := string(key.DeadlineID)
		dl = &s
	}
	if key.UserID != "" {
		user = &key.UserID
	}
	return []any{key.Kind, dl, user, dateArg(key.Day)}
}

// Sent implements SentLog.
func (l PGSentLog) Sent(ctx context.Context, key SentKey) (bool, error) {
	var done bool
	err := l.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sentSQL, sentArgs(key)...).Scan(&done)
	})
	if err != nil {
		return false, fmt.Errorf("reminder sent %s: %w", key, err)
	}
	return done, nil
}

// Mark implements SentLog. A key already marked is left as it is.
func (l PGSentLog) Mark(ctx context.Context, key SentKey) error {
	err := l.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, markSQL, sentArgs(key)...)
		return err
	})
	if err != nil {
		return fmt.Errorf("reminder mark %s: %w", key, err)
	}
	return nil
}
