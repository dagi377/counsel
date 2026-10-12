package comms

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// Message events (spec/eng/a2-interfaces.json, EVENTS-8). Bodies carry ids and times only,
// never the subject, body, sender or phone number.
const (
	EventEmailNeedsReply       = "email.needs_reply"
	EventTextReceived          = "text.received"
	EventPortalMessageReceived = "comms.portal_message_received"
)

// MessageEvent is the body of the three message events.
type MessageEvent struct {
	CommItemID string    `json:"comm_item_id"`
	MatterID   string    `json:"matter_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// MessageEvents publishes message events in the caller's transaction. The SMS and portal
// sync and the reply triage have no write point yet; they call it when they store or flag a
// comm item (Q-EVENTS-8).
type MessageEvents struct {
	Emitter outbox.Emitter
}

// NeedsReply publishes email.needs_reply for an inbound email flagged as awaiting a reply.
func (e MessageEvents) NeedsReply(ctx context.Context, tx pgx.Tx, m MessageEvent) error {
	return e.emit(ctx, tx, EventEmailNeedsReply, m)
}

// TextReceived publishes text.received for a stored inbound SMS.
func (e MessageEvents) TextReceived(ctx context.Context, tx pgx.Tx, m MessageEvent) error {
	return e.emit(ctx, tx, EventTextReceived, m)
}

// PortalMessageReceived publishes comms.portal_message_received for a stored portal message.
func (e MessageEvents) PortalMessageReceived(ctx context.Context, tx pgx.Tx, m MessageEvent) error {
	return e.emit(ctx, tx, EventPortalMessageReceived, m)
}

func (e MessageEvents) emit(ctx context.Context, tx pgx.Tx, typ string, m MessageEvent) error {
	if m.CommItemID == "" || m.OccurredAt.IsZero() {
		return fmt.Errorf("publish %s: %w", typ, ErrInvalid)
	}
	if e.Emitter == nil {
		return fmt.Errorf("publish %s: %w", typ, ErrNoWritePoint)
	}
	// rule: ARC-I-030
	if err := e.Emitter.Emit(ctx, tx, outbox.Subject{Kind: subjectCommItem, ID: m.CommItemID}, typ, m); err != nil {
		return fmt.Errorf("publish %s: %w", typ, err)
	}
	return nil
}
