// Package auditfake is an in-memory kernel/audit for tests: no network, no Postgres.
package auditfake

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/audit"
)

var (
	_ audit.Log    = (*Fake)(nil)
	_ audit.Masker = (*Fake)(nil)
)

// Masked replaces every detail value whose key is not in the fake's allow set.
const Masked = "[masked]"

// Fake keeps the chain in memory. The tx argument is ignored.
type Fake struct {
	mu     sync.Mutex
	events []audit.Event
	allow  map[string]bool
	// Now stamps Event.At; tests may replace it.
	Now func() time.Time
}

// New returns an empty chain. Detail keys in allowKeys are kept; every other value is masked.
func New(allowKeys ...string) *Fake {
	f := &Fake{allow: map[string]bool{}, Now: time.Now}
	for _, k := range allowKeys {
		f.allow[k] = true
	}
	return f
}

// Mask keeps allowed keys and replaces every other value with Masked.
func (f *Fake) Mask(detail map[string]string) map[string]string {
	if detail == nil {
		return nil
	}
	out := make(map[string]string, len(detail))
	for k, v := range detail {
		if f.allow[k] {
			out[k] = v
		} else {
			out[k] = Masked
		}
	}
	return out
}

// Append chains e to the last event.
func (f *Fake) Append(_ context.Context, _ pgx.Tx, e audit.Event) error {
	if e.ActorKind == "" || e.ActorID == "" || e.Action == "" {
		return fmt.Errorf("append %q: %w", e.Action, audit.ErrInvalidEvent)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e.Seq = audit.Seq(len(f.events) + 1)
	e.At = f.Now().UTC()
	e.Detail = f.Mask(e.Detail)
	e.PrevHash = nil
	if n := len(f.events); n > 0 {
		e.PrevHash = f.events[n-1].Hash
	}
	h, err := hash(e)
	if err != nil {
		return err
	}
	e.Hash = h
	f.events = append(f.events, e)
	return nil
}

// Verify recomputes from..to inclusive.
func (f *Fake) Verify(_ context.Context, from, to audit.Seq) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if from < 1 || from > to || int(to) > len(f.events) {
		return fmt.Errorf("verify %d..%d: %w", from, to, audit.ErrBadRange)
	}
	for s := from; s <= to; s++ {
		e := f.events[s-1]
		var prev []byte
		if s > 1 {
			prev = f.events[s-2].Hash
		}
		if !bytes.Equal(e.PrevHash, prev) {
			return fmt.Errorf("verify seq %d link: %w", s, audit.ErrChainBroken)
		}
		h, err := hash(e)
		if err != nil {
			return err
		}
		if !bytes.Equal(h, e.Hash) {
			return fmt.Errorf("verify seq %d hash: %w", s, audit.ErrChainBroken)
		}
	}
	return nil
}

// Events returns a copy of the chain.
func (f *Fake) Events() []audit.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]audit.Event(nil), f.events...)
}

// Tamper lets a test change a stored event in place, as an attacker with table access would.
func (f *Fake) Tamper(s audit.Seq, fn func(*audit.Event)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(&f.events[s-1])
}

// hash is sha256(prev_hash || canonical(row)); canonical is JSON with sorted detail keys.
func hash(e audit.Event) ([]byte, error) {
	row := struct {
		Seq       audit.Seq
		At        string
		ActorKind string
		ActorID   string
		ActingFor string
		Action    string
		Subject   audit.Subject
		Detail    map[string]string
	}{e.Seq, e.At.UTC().Format(time.RFC3339Nano), e.ActorKind, e.ActorID, e.ActingFor, e.Action, e.Subject, e.Detail}
	b, err := json.Marshal(row)
	if err != nil {
		return nil, fmt.Errorf("canonical event: %w", err)
	}
	h := sha256.New()
	h.Write(e.PrevHash)
	h.Write(b)
	return h.Sum(nil), nil
}
