package comms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/outbox"
)

// EventEmailReceived is published when an inbound email is stored (spec/eng/a2-interfaces.json,
// Q-EVENTS-WIRE-4).
const EventEmailReceived = "email.received"

// ErrNoWritePoint means the inbound store has no database or no emitter, so the event would
// be lost; it refuses instead of storing the email without it.
var ErrNoWritePoint = errors.New("comms: write point not wired")

// EmailReceived is the body of EventEmailReceived: ids and the time only, never the subject,
// body or addresses.
type EmailReceived struct {
	CommItemID  string    `json:"comm_item_id"`
	ConnectorID string    `json:"connector_id,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// InboundEmail is one synced inbound email. BodyRef is the object key of the encrypted body;
// the database never holds the body.
type InboundEmail struct {
	ConnectorID    string          // the mail connector, or empty
	ExternalID     string          // the provider's message id, unique per connector
	ThreadRef      string          // the provider's thread id, or empty
	Participants   json.RawMessage // a JSON array of participant objects
	DisplaySubject string
	BodyRef        string
	OccurredAt     time.Time
}

// Inbound stores inbound emails.
type Inbound struct {
	DB     db.DB
	Events outbox.Emitter
}

// receiveEmailSQL stores one inbound email; a message already stored for the connector
// returns no row.
const receiveEmailSQL = `INSERT INTO comms.comm_item
  (channel, direction, connector_id, external_id, thread_ref, participants, display_subject, body_ref, occurred_at)
VALUES ('email', 'inbound', NULLIF($1, '')::uuid, NULLIF($2, ''), NULLIF($3, ''), $4::jsonb, NULLIF($5, ''), NULLIF($6, ''), $7)
ON CONFLICT (connector_id, external_id) DO NOTHING
RETURNING id::text`

// Receive stores e and publishes EventEmailReceived in the same tx. It returns the comm item
// id, or empty when the connector already delivered the same message (nothing published).
func (s *Inbound) Receive(ctx context.Context, e InboundEmail) (string, error) {
	if e.OccurredAt.IsZero() || !json.Valid(e.Participants) || (e.ConnectorID != "" && e.ExternalID == "") {
		return "", fmt.Errorf("receive email: %w", ErrInvalid)
	}
	if s.DB == nil || s.Events == nil {
		return "", fmt.Errorf("receive email: %w", ErrNoWritePoint)
	}
	var id string
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, receiveEmailSQL, e.ConnectorID, e.ExternalID, e.ThreadRef,
			string(e.Participants), e.DisplaySubject, e.BodyRef, e.OccurredAt).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			id = ""
			return nil
		}
		if err != nil {
			return err
		}
		// rule: ARC-I-030
		body := EmailReceived{CommItemID: id, ConnectorID: e.ConnectorID, OccurredAt: e.OccurredAt}
		return s.Events.Emit(ctx, tx, outbox.Subject{Kind: subjectCommItem, ID: id}, EventEmailReceived, body)
	})
	if err != nil {
		return "", fmt.Errorf("receive email: %w", err)
	}
	return id, nil
}
