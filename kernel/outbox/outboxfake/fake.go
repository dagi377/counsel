// Package outboxfake is an in-memory outbox (Emitter, Relay, Inbox) for tests. It ignores
// the transaction argument. No network, Postgres or Docker.
package outboxfake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

var (
	_ outbox.Emitter = (*Fake)(nil)
	_ outbox.Relay   = (*Fake)(nil)
	_ outbox.Inbox   = (*Fake)(nil)
)

type inboxKey struct{ consumer, id string }

// Fake is a concurrency-safe in-memory outbox.
type Fake struct {
	// Now returns the current time; nil means time.Now.
	Now func() time.Time

	mu    sync.Mutex
	seq   int
	msgs  map[string]*outbox.Message
	order []string
	inbox map[inboxKey]bool
}

// New returns an empty Fake.
func New() *Fake { return &Fake{msgs: map[string]*outbox.Message{}, inbox: map[inboxKey]bool{}} }

func (f *Fake) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// Emit implements outbox.Emitter.
func (f *Fake) Emit(ctx context.Context, _ pgx.Tx, s outbox.Subject, typ string, body any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if typ == "" || s.Kind == "" || s.ID == "" {
		return fmt.Errorf("emit: %w", outbox.ErrInvalidEvent)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("emit: %w: %v", outbox.ErrInvalidEvent, err)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return fmt.Errorf("emit: %w: body is not a JSON object", outbox.ErrInvalidEvent)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.msgs == nil {
		f.msgs = map[string]*outbox.Message{}
	}
	f.seq++
	id := "msg-" + strconv.Itoa(f.seq)
	f.msgs[id] = &outbox.Message{ID: id, Type: typ, Subject: s, Payload: raw, CreatedAt: f.now()}
	f.order = append(f.order, id)
	return nil
}

// Pending implements outbox.Relay.
func (f *Fake) Pending(ctx context.Context, limit int) ([]outbox.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []outbox.Message
	for _, id := range f.order {
		m := f.msgs[id]
		if m.PublishedAt != nil {
			continue
		}
		if limit > 0 && len(out) >= limit {
			break
		}
		out = append(out, *m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// MarkPublished implements outbox.Relay.
func (f *Fake) MarkPublished(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.msgs[id]
	if !ok {
		return fmt.Errorf("mark published: %w", outbox.ErrNotFound)
	}
	if m.PublishedAt == nil {
		t := f.now()
		m.PublishedAt = &t
	}
	return nil
}

// MarkFailed implements outbox.Relay.
func (f *Fake) MarkFailed(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.msgs[id]
	if !ok {
		return fmt.Errorf("mark failed: %w", outbox.ErrNotFound)
	}
	m.Attempts++
	return nil
}

// Claim implements outbox.Inbox.
func (f *Fake) Claim(ctx context.Context, _ pgx.Tx, consumer, messageID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if consumer == "" || messageID == "" {
		return false, fmt.Errorf("claim: %w", outbox.ErrInvalidEvent)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inbox == nil {
		f.inbox = map[inboxKey]bool{}
	}
	k := inboxKey{consumer, messageID}
	if f.inbox[k] {
		return false, nil
	}
	f.inbox[k] = true
	return true, nil
}

// Messages returns every emitted message, published or not, in emit order.
func (f *Fake) Messages() []outbox.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]outbox.Message, 0, len(f.order))
	for _, id := range f.order {
		out = append(out, *f.msgs[id])
	}
	return out
}
