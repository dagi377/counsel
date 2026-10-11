// Package webhookfake is an in-memory kernel/webhook for tests: no network, no Postgres.
package webhookfake

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/webhook"
)

var (
	_ webhook.Signer     = (*Fake)(nil)
	_ webhook.Dispatcher = (*Fake)(nil)
)

// Tolerance is the allowed distance between webhook-timestamp and now.
const Tolerance = 5 * time.Minute

type queued struct {
	d     webhook.Delivery
	acked bool
}

// Fake signs with HMAC-SHA256 and keeps queues in memory. The tx argument is ignored.
type Fake struct {
	mu        sync.Mutex
	types     map[string]bool
	endpoints map[string]webhook.Endpoint
	order     []string
	queue     []*queued
	n         int
}

// New returns an empty dispatcher.
func New() *Fake {
	return &Fake{types: map[string]bool{}, endpoints: map[string]webhook.Endpoint{}}
}

// Sign returns "v1,<base64 HMAC-SHA256(secret, id.ts.body)>".
func (f *Fake) Sign(secret []byte, msgID string, ts time.Time, body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(msgID + "." + strconv.FormatInt(ts.Unix(), 10) + "."))
	m.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// Verify accepts the header when any v1 signature matches and ts is within Tolerance.
func (f *Fake) Verify(secret []byte, msgID string, ts, now time.Time, body []byte, header string) error {
	if d := now.Sub(ts); d > Tolerance || d < -Tolerance {
		return fmt.Errorf("verify %s: %w", msgID, webhook.ErrTimestampSkew)
	}
	want := f.Sign(secret, msgID, ts, body)
	for _, s := range strings.Fields(header) {
		if hmac.Equal([]byte(s), []byte(want)) {
			return nil
		}
	}
	return fmt.Errorf("verify %s: %w", msgID, webhook.ErrBadSignature)
}

// RegisterType registers an event type.
func (f *Fake) RegisterType(t webhook.EventType) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.Name == "" {
		return fmt.Errorf("register type: %w", webhook.ErrUnknownType)
	}
	if f.types[t.Name] {
		return fmt.Errorf("register type %s: %w", t.Name, webhook.ErrDuplicateType)
	}
	f.types[t.Name] = true
	return nil
}

// AddEndpoint registers a subscriber.
func (f *Fake) AddEndpoint(_ context.Context, e webhook.Endpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.endpoints[e.ID]; !ok {
		f.order = append(f.order, e.ID)
	}
	f.endpoints[e.ID] = e
	return nil
}

func subscribed(e webhook.Endpoint, typ string) bool {
	if len(e.Types) == 0 {
		return true
	}
	for _, t := range e.Types {
		if t == typ {
			return true
		}
	}
	return false
}

// Enqueue queues m for each subscribed endpoint. The fake checks only that the payload is JSON.
func (f *Fake) Enqueue(_ context.Context, _ pgx.Tx, m webhook.Message) ([]webhook.DeliveryID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.types[m.Type] {
		return nil, fmt.Errorf("enqueue %s: %w", m.Type, webhook.ErrUnknownType)
	}
	if !json.Valid(m.Payload) {
		return nil, fmt.Errorf("enqueue %s: %w", m.Type, webhook.ErrInvalidPayload)
	}
	var ids []webhook.DeliveryID
	for _, eid := range f.order {
		if !subscribed(f.endpoints[eid], m.Type) {
			continue
		}
		f.n++
		id := webhook.DeliveryID("dlv-" + strconv.Itoa(f.n))
		f.queue = append(f.queue, &queued{d: webhook.Delivery{ID: id, Endpoint: eid, Message: m}})
		ids = append(ids, id)
	}
	return ids, nil
}

// Ready returns the oldest unacked delivery per subject for endpoint.
func (f *Fake) Ready(_ context.Context, endpoint string) ([]webhook.Delivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.endpoints[endpoint]; !ok {
		return nil, fmt.Errorf("ready %s: %w", endpoint, webhook.ErrUnknownEndpoint)
	}
	seen := map[webhook.Subject]bool{}
	var out []webhook.Delivery
	for _, q := range f.queue {
		if q.acked || q.d.Endpoint != endpoint || seen[q.d.Message.Subject] {
			continue
		}
		seen[q.d.Message.Subject] = true
		out = append(out, q.d)
	}
	return out, nil
}

func (f *Fake) find(id webhook.DeliveryID) (*queued, error) {
	for _, q := range f.queue {
		if q.d.ID == id {
			return q, nil
		}
	}
	return nil, fmt.Errorf("delivery %s: %w", id, webhook.ErrNotFound)
}

// Ack marks a delivery sent.
func (f *Fake) Ack(_ context.Context, id webhook.DeliveryID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	q, err := f.find(id)
	if err != nil {
		return err
	}
	q.acked = true
	q.d.Attempts++
	return nil
}

// Fail counts an attempt and keeps the delivery at the head of its subject's queue.
func (f *Fake) Fail(_ context.Context, id webhook.DeliveryID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	q, err := f.find(id)
	if err != nil {
		return err
	}
	q.d.Attempts++
	return nil
}
