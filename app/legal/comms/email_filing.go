package comms

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/outbox"
)

// EventEmailFiled is the outbox event comms emits when an email is filed to a matter (a2
// events: email.filed, docs/QUESTIONS.md Q-DRAFT-FIX). Deadline extraction (F-DL-01) starts
// from it as it does from document.uploaded.
const EventEmailFiled = "email.filed"

// subjectCommItem is the outbox subject kind of email.filed.
const subjectCommItem = "comm_item"

// EmailFiled is the email.filed payload. It holds ids only, never the subject, body or
// participants of the email.
type EmailFiled struct {
	CommItemID string `json:"comm_item_id"`
	MatterID   string `json:"matter_id"`
	FiledBy    string `json:"filed_by"`
}

// EmailFilingStore files email messages (comms.comm_item, channel email) to matters on
// comms.comm_filing and emits email.filed with the change.
type EmailFilingStore struct {
	db  db.DB
	out outbox.Emitter
}

// NewEmailFilingStore returns a store on pool that emits through out.
func NewEmailFilingStore(pool db.DB, out outbox.Emitter) *EmailFilingStore {
	return &EmailFilingStore{db: pool, out: out}
}

const (
	// sqlFileEmail files the email: a new filing row, or a proposed or rejected one turned
	// filed. A row already filed is left as it is and returns nothing.
	sqlFileEmail = `INSERT INTO comms.comm_filing (comm_item_id, matter_id, status, filed_by, filed_at)
SELECT id, $2, 'filed', $3, now() FROM comms.comm_item
WHERE id = $1 AND channel = 'email' AND excluded_hash IS NULL
ON CONFLICT (comm_item_id, matter_id) DO UPDATE
  SET status = 'filed', filed_by = EXCLUDED.filed_by, filed_at = EXCLUDED.filed_at
  WHERE comm_filing.status <> 'filed'
RETURNING comm_item_id::text`
	sqlEmailFiled = `SELECT EXISTS (SELECT 1 FROM comms.comm_filing f
  JOIN comms.comm_item i ON i.id = f.comm_item_id
  WHERE f.comm_item_id = $1 AND f.matter_id = $2 AND f.status = 'filed' AND i.channel = 'email')`
)

// FileEmail files email commItemID to matterID, filed by filedBy, and emits email.filed in
// the same transaction. It reports whether this call filed it: filing an email already filed
// to the matter changes nothing and emits nothing. An unknown email, or a message on another
// channel, returns ErrNotFound.
func (s *EmailFilingStore) FileEmail(ctx context.Context, commItemID, matterID, filedBy string) (bool, error) {
	ev := EmailFiled{
		CommItemID: strings.TrimSpace(commItemID),
		MatterID:   strings.TrimSpace(matterID),
		FiledBy:    strings.TrimSpace(filedBy),
	}
	switch {
	case ev.CommItemID == "":
		return false, fmt.Errorf("file email: comm item: %w", ErrInvalid)
	case ev.MatterID == "":
		return false, fmt.Errorf("file email: matter: %w", ErrInvalid)
	case ev.FiledBy == "":
		return false, fmt.Errorf("file email: filed by: %w", ErrInvalid)
	case s.db == nil || s.out == nil:
		return false, fmt.Errorf("file email: store not wired: %w", ErrInvalid)
	}
	filed := false
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, sqlFileEmail, ev.CommItemID, ev.MatterID, ev.FiledBy).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			var already bool
			if err := tx.QueryRow(ctx, sqlEmailFiled, ev.CommItemID, ev.MatterID).Scan(&already); err != nil {
				return err
			}
			if !already {
				return ErrNotFound
			}
			return nil
		}
		if err != nil {
			return err
		}
		filed = true
		// rule: ARC-I-030
		return s.out.Emit(ctx, tx, outbox.Subject{Kind: subjectCommItem, ID: ev.CommItemID}, EventEmailFiled, ev)
	})
	if err != nil {
		return false, fmt.Errorf("file email: %w", err)
	}
	return filed, nil
}
