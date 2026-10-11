// CONTRACT: kernel/idem. The shape of this file is frozen after wave 0; changes are additive
// only and need a docs/QUESTIONS.md entry. Bodies live in the T-008 subtasks.

package idem

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// HeaderName is the request header that carries the client's idempotency key.
const HeaderName = "Idempotency-Key"

// Key length bounds, matching platform.idempotency_key.key.
const (
	MinKeyLen = 8
	MaxKeyLen = 200
)

// DefaultTTL is how long a stored result is replayed before the key may be reused.
const DefaultTTL = 24 * time.Hour

// Sentinel errors.
var (
	// ErrInvalidKey: the key is empty or outside MinKeyLen..MaxKeyLen.
	ErrInvalidKey = errors.New("idem: invalid idempotency key")
	// ErrKeyReused: the key was seen with a different request body (HTTP 422).
	ErrKeyReused = errors.New("idem: key reused with a different request")
	// ErrInFlight: the first request with this key has not finished yet (HTTP 409).
	ErrInFlight = errors.New("idem: request with this key is still in progress")
	// ErrNotFound: no reservation exists for the key.
	ErrNotFound = errors.New("idem: key not found")
)

// Key scopes an idempotency key to the principal that sent it.
type Key struct {
	PrincipalID string
	Value       string
}

// Response is the stored result of the first request, replayed for repeats.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// Store reserves keys and records results. One reservation per Key; a reservation expires
// after its TTL.
type Store interface {
	// Begin reserves k for a request whose canonical body hashes to requestSHA256.
	// It returns (nil, nil) when the caller should run the request, the stored Response when
	// the same request already completed, ErrKeyReused when the hash differs, and ErrInFlight
	// when the first request is still running.
	Begin(ctx context.Context, k Key, requestSHA256 [32]byte, ttl time.Duration) (*Response, error)
	// Complete stores the result for a key reserved by Begin.
	Complete(ctx context.Context, k Key, resp Response) error
	// Release drops an unfinished reservation so the client may retry, for example after a
	// 5xx or a cancelled request. Completed results are kept.
	Release(ctx context.Context, k Key) error
}

// Middleware wraps money and external-effect POST handlers: it reads HeaderName, hashes the
// body, calls Store.Begin and replays or records the response.
type Middleware func(next http.Handler) http.Handler
