package inbound

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/registry"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// MaxBody is the largest webhook body accepted.
const MaxBody = 1 << 20

// providerID is the {provider} path segment shape from api/openapi.yaml.
var providerID = regexp.MustCompile(`^[a-z0-9_]{2,40}$`)

// TxRunner runs fn in one transaction (kernel/db.DB satisfies it).
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// PG is the Postgres Receiver over platform.inbound_event.
type PG struct {
	db  TxRunner
	now func() time.Time

	mu        sync.RWMutex
	providers map[string]Provider
}

var _ Receiver = (*PG)(nil)

// NewPG returns a receiver that records events through db; now nil means time.Now.
func NewPG(db TxRunner, now func() time.Time) *PG {
	if now == nil {
		now = time.Now
	}
	return &PG{db: db, now: now, providers: map[string]Provider{}}
}

// FromRegistry adapts a provider the app registered through Registry.InboundProvider.
func FromRegistry(p registry.Provider) Provider {
	out := Provider{ID: p.ID, Verify: p.Verify}
	if p.Handle != nil {
		out.Handle = func(ctx context.Context, tx pgx.Tx, e InboundEvent) error {
			return p.Handle(ctx, tx, registry.InboundEvent{Provider: e.Provider, EventID: e.EventID, Body: e.Body, ReceivedAt: e.ReceivedAt})
		}
	}
	return out
}

// Register adds a provider. The kernel registers none; the app's providers arrive here
// through Registry.InboundProvider at boot.
func (s *PG) Register(p Provider) error {
	if !providerID.MatchString(p.ID) || p.Verify == nil || p.Handle == nil {
		return fmt.Errorf("register provider %q: id, Verify and Handle are required", p.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.providers[p.ID]; ok {
		return fmt.Errorf("register provider %q: %w", p.ID, ErrDuplicateProvider)
	}
	s.providers[p.ID] = p
	return nil
}

// Receive verifies one delivery, records (provider, event_id) and runs Handle in the same
// tx the first time only.
func (s *PG) Receive(ctx context.Context, provider string, r *http.Request, body []byte) (Result, error) {
	s.mu.RLock()
	p, ok := s.providers[provider]
	s.mu.RUnlock()
	if !ok {
		return Result{}, fmt.Errorf("receive %q: %w", provider, ErrUnknownProvider)
	}
	// rule: ARC-U-033
	eventID, err := p.Verify(r, body)
	if err != nil {
		return Result{}, fmt.Errorf("receive %q: verify: %w", provider, err)
	}
	if eventID == "" {
		return Result{}, fmt.Errorf("receive %q: %w", provider, ErrNoEventID)
	}
	e := InboundEvent{Provider: provider, EventID: eventID, ReceivedAt: s.now().UTC(), PayloadSHA256: sha256.Sum256(body), Body: body}
	res := Result{EventID: eventID}
	err = s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// rule: BIL-I-020
		// The (provider, event_id) row is written before any effect, for every event type;
		// a second delivery finds it and does nothing.
		tag, err := tx.Exec(ctx, `
INSERT INTO platform.inbound_event (provider, event_id, received_at, payload_sha256)
VALUES ($1, $2, $3, $4)
ON CONFLICT (provider, event_id) DO NOTHING`, e.Provider, e.EventID, e.ReceivedAt, e.PayloadSHA256[:])
		if err != nil {
			return fmt.Errorf("record event: %w", err)
		}
		if tag.RowsAffected() == 0 {
			res.Duplicate = true
			return nil
		}
		if err := p.Handle(ctx, tx, e); err != nil {
			return fmt.Errorf("handle event: %w", err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE platform.inbound_event SET processed_at = now() WHERE provider = $1 AND event_id = $2`, e.Provider, e.EventID); err != nil {
			return fmt.Errorf("mark processed: %w", err)
		}
		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("receive %q: %w", provider, err)
	}
	return res, nil
}

// Routes mounts POST /hooks/{provider} on r.
func (s *PG) Routes(r chi.Router) {
	r.Post("/hooks/{provider}", s.ServeHook)
}

// ServeHook handles POST /hooks/{provider}. It answers 204 for a new or duplicate event,
// 401 for a bad signature or stale timestamp, and 503 when the event could not be stored,
// so the provider retries; it never answers 2xx without a stored row.
func (s *PG) ServeHook(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		http.Error(w, "body too large or unreadable", http.StatusBadRequest)
		return
	}
	res, err := s.Receive(r.Context(), provider, r, body)
	// rule: ARC-U-033
	switch {
	case err == nil:
		if res.Duplicate {
			slog.InfoContext(r.Context(), "inbound duplicate ignored", "provider", provider)
		}
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrUnknownProvider):
		http.Error(w, "unknown provider", http.StatusNotFound)
	case errors.Is(err, ErrBadSignature), errors.Is(err, ErrStaleTimestamp):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, ErrNoEventID):
		http.Error(w, "missing event id", http.StatusBadRequest)
	default:
		slog.ErrorContext(r.Context(), "inbound event not stored", "provider", provider, "err", err)
		http.Error(w, "try again", http.StatusServiceUnavailable)
	}
}
