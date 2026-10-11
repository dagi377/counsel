// CONTRACT: kernel/db (T-043). Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Source: spec/eng/a1-uptime (ARC-U-011: session state is SET LOCAL
// only, so a pooled connection carries nothing into the next request; River connects
// directly, not through the pooler). Bodies live in the T-043 subtasks (pool.go).

package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Session variable names set with SET LOCAL at the start of every transaction. Row-level
// security policies read them.
const (
	VarUserID        = "counsel.user_id"
	VarPrincipalKind = "counsel.principal_kind"
	VarRunID         = "counsel.run_id"
	VarActingFor     = "counsel.acting_for"
)

// Session is the per-transaction identity the database sees.
type Session struct {
	UserID        string
	PrincipalKind string // "user", "agent", "guest" or "system"
	RunID         string // agent only
	ActingFor     string // agent only
}

// Vars returns the session as SET LOCAL name/value pairs; empty fields are omitted.
func (s Session) Vars() map[string]string {
	out := map[string]string{}
	for k, v := range map[string]string{VarUserID: s.UserID, VarPrincipalKind: s.PrincipalKind, VarRunID: s.RunID, VarActingFor: s.ActingFor} {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrClosed means the pool is closed.
	ErrClosed = errors.New("db: pool closed")
	// ErrNoTx means a session was bound outside a transaction from this pool.
	ErrNoTx = errors.New("db: not a transaction from this pool")
)

// DB runs work in transactions on a pooled connection.
type DB interface {
	// InTx begins a transaction, runs fn, and commits when fn returns nil; otherwise it rolls
	// back and returns fn's error.
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
	// Bind sets the session variables with SET LOCAL; they vanish when tx ends.
	Bind(ctx context.Context, tx pgx.Tx, s Session) error
	// Ping checks the database is reachable.
	Ping(ctx context.Context) error
	// Close releases the pool.
	Close()
}
