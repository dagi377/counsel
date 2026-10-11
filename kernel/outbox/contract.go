// CONTRACT: kernel/outbox. The shape of this file is frozen after wave 0; changes are additive
// only and need a docs/QUESTIONS.md entry. Bodies live in the T-008 subtasks.

package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Sentinel errors.
var (
	// ErrInvalidEvent: empty type, empty subject, or a body that does not encode as a JSON object.
	ErrInvalidEvent = errors.New("outbox: invalid event")
	// ErrNotFound: no message with that id.
	ErrNotFound = errors.New("outbox: message not found")
)

// Subject is the aggregate an event is about (platform.outbox.aggregate, aggregate_id).
type Subject struct {
	Kind string
	ID   string
}

// Message is one row of platform.outbox. Payload holds ids and amounts only, never
// document text, notes or client names.
type Message struct {
	ID          string
	Type        string
	Subject     Subject
	Payload     json.RawMessage
	CreatedAt   time.Time
	PublishedAt *time.Time
	Attempts    int
}

// Emitter writes an event in the caller's transaction, so the change and its event commit
// together.
type Emitter interface {
	Emit(ctx context.Context, tx pgx.Tx, s Subject, typ string, body any) error
}

// Relay reads unpublished messages in creation order and records delivery.
type Relay interface {
	// Pending returns up to limit unpublished messages, oldest first.
	Pending(ctx context.Context, limit int) ([]Message, error)
	// MarkPublished sets published_at; a second call is a no-op.
	MarkPublished(ctx context.Context, id string) error
	// MarkFailed increments attempts and leaves the message pending.
	MarkFailed(ctx context.Context, id string) error
}

// Inbox dedups consumer-side effects per (consumer, message id) (platform.inbox).
type Inbox interface {
	// Claim records that consumer handles messageID in tx. first is false when the message
	// was already processed, and the caller must then skip the effect.
	Claim(ctx context.Context, tx pgx.Tx, consumer, messageID string) (first bool, err error)
}

// Handler consumes one message for a named consumer.
type Handler func(ctx context.Context, tx pgx.Tx, m Message) error
