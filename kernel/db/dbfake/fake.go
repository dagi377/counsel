// Package dbfake is an in-memory kernel/db for tests: no network, no Postgres.
package dbfake

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

var _ db.DB = (*Fake)(nil)

// Tx is the transaction InTx hands to fn. Only its session variables are real; calling any
// pgx.Tx method on it panics, because the fake has no database.
type Tx struct {
	pgx.Tx
	vars map[string]string
}

// Vars returns the session variables set on tx.
func (t *Tx) Vars() map[string]string { return maps.Clone(t.vars) }

// Fake counts commits and rollbacks. Session variables live only on the Tx, like SET LOCAL.
type Fake struct {
	mu        sync.Mutex
	closed    bool
	Commits   int
	Rollbacks int
}

// New returns an open fake pool.
func New() *Fake { return &Fake{} }

// InTx runs fn on a fresh Tx with no session variables.
func (f *Fake) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	if closed {
		return fmt.Errorf("begin: %w", db.ErrClosed)
	}
	err := fn(ctx, &Tx{vars: map[string]string{}})
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		f.Rollbacks++
		return err
	}
	f.Commits++
	return nil
}

// Bind sets s on tx.
func (f *Fake) Bind(_ context.Context, tx pgx.Tx, s db.Session) error {
	t, ok := tx.(*Tx)
	if !ok || t == nil {
		return fmt.Errorf("bind: %w", db.ErrNoTx)
	}
	maps.Copy(t.vars, s.Vars())
	return nil
}

// Ping fails once the pool is closed.
func (f *Fake) Ping(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return fmt.Errorf("ping: %w", db.ErrClosed)
	}
	return nil
}

// Close closes the pool.
func (f *Fake) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}
