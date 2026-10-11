// Package inboundfake is an in-memory implementation of the kernel/inbound contract for tests.
package inboundfake

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/inbound"
)

var _ inbound.Receiver = (*Fake)(nil)

type key struct{ provider, eventID string }

// Fake keeps providers and recorded events in memory. Handle is called with a nil tx.
// A Fake is safe for concurrent use.
type Fake struct {
	mu        sync.Mutex
	now       func() time.Time
	providers map[string]inbound.Provider
	events    map[key]inbound.InboundEvent
}

// New returns an empty Fake that stamps events with now.
func New(now func() time.Time) *Fake {
	if now == nil {
		now = time.Now
	}
	return &Fake{now: now, providers: map[string]inbound.Provider{}, events: map[key]inbound.InboundEvent{}}
}

// Register adds a provider.
func (f *Fake) Register(p inbound.Provider) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.ID == "" {
		return fmt.Errorf("register: empty id: %w", inbound.ErrUnknownProvider)
	}
	if _, ok := f.providers[p.ID]; ok {
		return fmt.Errorf("register %q: %w", p.ID, inbound.ErrDuplicateProvider)
	}
	f.providers[p.ID] = p
	return nil
}

// Receive verifies, dedups and dispatches one delivery.
func (f *Fake) Receive(ctx context.Context, provider string, r *http.Request, body []byte) (inbound.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.providers[provider]
	if !ok {
		return inbound.Result{}, fmt.Errorf("receive %q: %w", provider, inbound.ErrUnknownProvider)
	}
	id, err := p.Verify(r, body)
	if err != nil {
		return inbound.Result{}, fmt.Errorf("receive %q: verify: %w", provider, err)
	}
	if id == "" {
		return inbound.Result{}, fmt.Errorf("receive %q: %w", provider, inbound.ErrNoEventID)
	}
	k := key{provider, id}
	if _, seen := f.events[k]; seen {
		return inbound.Result{EventID: id, Duplicate: true}, nil
	}
	e := inbound.InboundEvent{
		Provider:      provider,
		EventID:       id,
		ReceivedAt:    f.now().UTC(),
		PayloadSHA256: sha256.Sum256(body),
		Body:          body,
	}
	if p.Handle != nil {
		if err := p.Handle(ctx, nil, e); err != nil {
			// The row and the effect share a transaction: a failed effect records nothing.
			return inbound.Result{}, fmt.Errorf("receive %q: handle: %w", provider, err)
		}
	}
	e.Body = nil
	f.events[k] = e
	return inbound.Result{EventID: id}, nil
}

// Events returns the number of recorded (provider, event_id) rows.
func (f *Fake) Events() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}
