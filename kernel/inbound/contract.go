// CONTRACT: kernel/inbound. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-008b subtasks (inbound.go, verify.go).

package inbound

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// Sentinel errors.
var (
	// ErrUnknownProvider is returned for a webhook whose provider was never registered.
	ErrUnknownProvider = errors.New("inbound: unknown provider")
	// ErrDuplicateProvider is returned when a provider id is registered twice.
	ErrDuplicateProvider = errors.New("inbound: duplicate provider")
	// ErrBadSignature is returned when Verify rejects the request signature (HTTP 401).
	ErrBadSignature = errors.New("inbound: bad signature")
	// ErrStaleTimestamp is returned when the signed timestamp is outside the allowed skew (HTTP 401).
	ErrStaleTimestamp = errors.New("inbound: timestamp outside tolerance")
	// ErrNoEventID is returned when Verify accepts a request but yields an empty event id.
	ErrNoEventID = errors.New("inbound: empty event id")
)

// MaxSkew is how far a signed webhook timestamp may be from now before it is refused.
const MaxSkew = 5 * time.Minute

// InboundEvent is one verified, first-seen webhook delivery, handed to Provider.Handle.
type InboundEvent struct {
	Provider      string    // provider id, e.g. "payments.example"
	EventID       string    // provider's event id; (Provider, EventID) is the dedup key
	ReceivedAt    time.Time // when the kernel received it (UTC)
	PayloadSHA256 [32]byte  // hash of the raw body; the body itself is not stored
	Body          []byte    // raw request body, for Handle to decode
}

// Provider is an inbound webhook source. The app registers providers at boot through
// Registry.InboundProvider; the kernel registers none.
type Provider struct {
	// ID is the provider id; it is the {provider} path segment of POST /hooks/{provider}.
	ID string
	// Verify checks signature and timestamp and returns the provider's event id. It
	// returns an error wrapping ErrBadSignature or ErrStaleTimestamp to refuse the request.
	Verify func(r *http.Request, body []byte) (eventID string, err error)
	// Handle applies the event's effect inside the same transaction that records the
	// (provider, event_id) row, so a duplicate delivery never has a second effect.
	Handle func(ctx context.Context, tx pgx.Tx, e InboundEvent) error
}

// Result reports what Receive did with one delivery.
type Result struct {
	EventID   string
	Duplicate bool // true when (provider, event_id) was already recorded; Handle was not called
}

// Receiver records and dispatches inbound webhooks, deduplicated on (provider, event_id)
// for every event type.
type Receiver interface {
	// Register adds a provider. A duplicate or empty id is an error.
	Register(p Provider) error
	// Receive verifies one delivery, records it once and calls the provider's Handle the
	// first time only. A duplicate returns Result{Duplicate: true} and no error.
	Receive(ctx context.Context, provider string, r *http.Request, body []byte) (Result, error)
}
