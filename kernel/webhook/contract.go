// CONTRACT: kernel/webhook (T-036). Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Source: spec/eng/k1-kernel.html ("kernel/outbox, idem, inbound,
// webhook"). Outbound webhooks follow Standard Webhooks (webhook-id, webhook-timestamp,
// webhook-signature "v1,<base64 HMAC-SHA256>"), are delivered in order per (endpoint,
// subject), and carry only registered event types. Bodies live in the T-036 subtasks
// (sign.go, deliver.go, types.go).

package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Standard Webhooks header names.
const (
	HeaderID        = "webhook-id"
	HeaderTimestamp = "webhook-timestamp"
	HeaderSignature = "webhook-signature"
)

// Subject is an opaque reference to a business object (registry.Subject shape).
type Subject struct {
	Kind string
	ID   string
}

// EventType is a module-registered event type with the JSON Schema of its payload.
type EventType struct {
	Name   string // module-prefixed, e.g. "work.item_created"
	Schema []byte
}

// Endpoint is a subscriber URL. Secret is the raw signing key; it never reaches logs,
// errors, events or audit detail.
type Endpoint struct {
	ID     string
	URL    string
	Secret []byte
	Types  []string // subscribed event types; empty means all
}

// Message is one event to deliver.
type Message struct {
	ID        string // webhook-id; stable across retries
	Type      string
	Subject   Subject
	Timestamp time.Time
	Payload   json.RawMessage
}

// DeliveryID identifies one message queued for one endpoint.
type DeliveryID string

// Delivery is a queued message for an endpoint.
type Delivery struct {
	ID       DeliveryID
	Endpoint string
	Message  Message
	Attempts int
}

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrUnknownType means the event type was never registered; it cannot be emitted.
	ErrUnknownType = errors.New("webhook: unknown event type")
	// ErrDuplicateType means the event type is already registered.
	ErrDuplicateType = errors.New("webhook: duplicate event type")
	// ErrInvalidPayload means the payload does not match the type's schema.
	ErrInvalidPayload = errors.New("webhook: invalid payload")
	// ErrUnknownEndpoint means no endpoint has that id.
	ErrUnknownEndpoint = errors.New("webhook: unknown endpoint")
	// ErrNotFound means the delivery does not exist.
	ErrNotFound = errors.New("webhook: delivery not found")
	// ErrBadSignature means no v1 signature in the header matches.
	ErrBadSignature = errors.New("webhook: bad signature")
	// ErrTimestampSkew means the webhook-timestamp is outside the allowed tolerance.
	ErrTimestampSkew = errors.New("webhook: timestamp outside tolerance")
)

// Signer signs and verifies Standard Webhooks messages over "<id>.<unix ts>.<body>".
type Signer interface {
	// Sign returns the webhook-signature header value "v1,<base64>".
	Sign(secret []byte, msgID string, ts time.Time, body []byte) string
	// Verify checks a webhook-signature header (one or more space-separated signatures) and
	// that ts is within tolerance of now.
	Verify(secret []byte, msgID string, ts, now time.Time, body []byte, header string) error
}

// Dispatcher queues messages and hands them out in order per (endpoint, subject): message 2
// for a subject is not handed out until message 1 is acked.
type Dispatcher interface {
	// RegisterType registers an event type at boot.
	RegisterType(t EventType) error
	// AddEndpoint registers a subscriber.
	AddEndpoint(ctx context.Context, e Endpoint) error
	// Enqueue queues m for every subscribed endpoint inside tx.
	Enqueue(ctx context.Context, tx pgx.Tx, m Message) ([]DeliveryID, error)
	// Ready returns the deliveries for endpoint that may be sent now: at most one per subject.
	Ready(ctx context.Context, endpoint string) ([]Delivery, error)
	// Ack marks a delivery sent, releasing the next one for its subject.
	Ack(ctx context.Context, id DeliveryID) error
	// Fail records a failed attempt; the delivery stays at the head of its subject's queue.
	Fail(ctx context.Context, id DeliveryID) error
}
