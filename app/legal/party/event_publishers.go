// Minimal publishers for the kernel events this package's agent features bind to but no
// write path publishes yet (EVENTS-9, docs/QUESTIONS.md Q-EVENTS-9). The owning write path
// calls its publisher in the same transaction when it lands. Bodies carry ids, days, counts
// and codes only, plus user_id: the user an event run acts for (trigger.PayloadActor).

package party

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrEventBody means an event body lacks a required id; nothing is published.
var ErrEventBody = errors.New("party: event body missing a required field")

// Kernel events published here that no feature file of this package declares.
const (
	EventContactCreated = "contact.created" // a contact was created
)

// ContactCreated is the body of EventContactCreated: a contact was created.
type ContactCreated struct {
	ContactID string `json:"contact_id"`
	Kind      string `json:"kind"`
	UserID    string `json:"user_id"`
}

// PublishContactCreated publishes EventContactCreated in tx (nil: the emitter opens its own).
func PublishContactCreated(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b ContactCreated) error {
	if b.ContactID == "" || b.Kind == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventContactCreated, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "contact", ID: b.ContactID}, EventContactCreated, b)
}

// publishEvent emits one event; a nil emitter publishes nothing.
func publishEvent(ctx context.Context, em outbox.Emitter, tx pgx.Tx, s outbox.Subject, typ string, body any) error {
	if em == nil {
		return nil
	}
	// rule: ARC-I-030
	if err := em.Emit(ctx, tx, s, typ, body); err != nil {
		return fmt.Errorf("publish %s: %w", typ, err)
	}
	return nil
}
