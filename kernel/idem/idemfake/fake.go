// Package idemfake is an in-memory idem.Store for tests. No network, Postgres or Docker.
package idemfake

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/idem"
)

var _ idem.Store = (*Fake)(nil)

type entry struct {
	hash      [32]byte
	resp      *idem.Response
	expiresAt time.Time
}

// Fake is a concurrency-safe in-memory idem.Store.
type Fake struct {
	// Now returns the current time; nil means time.Now.
	Now func() time.Time

	mu      sync.Mutex
	entries map[idem.Key]*entry
}

// New returns an empty Fake.
func New() *Fake { return &Fake{entries: map[idem.Key]*entry{}} }

func (f *Fake) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func validKey(k idem.Key) error {
	if k.PrincipalID == "" || len(k.Value) < idem.MinKeyLen || len(k.Value) > idem.MaxKeyLen {
		return fmt.Errorf("begin: %w", idem.ErrInvalidKey)
	}
	return nil
}

// Begin implements idem.Store.
func (f *Fake) Begin(ctx context.Context, k idem.Key, requestSHA256 [32]byte, ttl time.Duration) (*idem.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validKey(k); err != nil {
		return nil, err
	}
	if ttl <= 0 {
		ttl = idem.DefaultTTL
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.entries == nil {
		f.entries = map[idem.Key]*entry{}
	}
	now := f.now()
	if e, ok := f.entries[k]; ok && now.Before(e.expiresAt) {
		switch {
		case e.hash != requestSHA256:
			return nil, fmt.Errorf("begin: %w", idem.ErrKeyReused)
		case e.resp == nil:
			return nil, fmt.Errorf("begin: %w", idem.ErrInFlight)
		default:
			return cloneResponse(e.resp), nil
		}
	}
	f.entries[k] = &entry{hash: requestSHA256, expiresAt: now.Add(ttl)}
	return nil, nil
}

// Complete implements idem.Store.
func (f *Fake) Complete(ctx context.Context, k idem.Key, resp idem.Response) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[k]
	if !ok {
		return fmt.Errorf("complete: %w", idem.ErrNotFound)
	}
	e.resp = cloneResponse(&resp)
	return nil
}

// Release implements idem.Store.
func (f *Fake) Release(ctx context.Context, k idem.Key) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[k]
	if !ok {
		return fmt.Errorf("release: %w", idem.ErrNotFound)
	}
	if e.resp == nil {
		delete(f.entries, k)
	}
	return nil
}

func cloneResponse(r *idem.Response) *idem.Response {
	c := &idem.Response{StatusCode: r.StatusCode, Body: append([]byte(nil), r.Body...)}
	if r.Header != nil {
		c.Header = http.Header{}
		for k, v := range r.Header {
			c.Header[k] = append([]string(nil), v...)
		}
	}
	return c
}
