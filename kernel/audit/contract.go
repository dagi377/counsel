// CONTRACT: kernel/audit (T-005). Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Source: spec/eng/k1-kernel.html ("kernel/audit": Append, Verify).
// Bodies live in the T-005 subtasks (audit.go, chain.go, verify.go, mask.go).

package audit

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Seq is the position of an event in the append-only audit chain. The first event is 1.
type Seq int64

// Subject is an opaque reference to a business object; the kernel never interprets Kind or
// ID. It has the shape of registry.Subject and becomes an alias of it once kernel/registry
// (T-002) is on main.
type Subject struct {
	Kind string
	ID   string
}

// Event is one audit record. Append fills Seq, At, PrevHash and Hash; callers set the rest.
// Detail is masked before it is stored (Masker): it never holds document text, customer
// names, amounts tied to a named customer or secrets.
type Event struct {
	Seq       Seq
	At        time.Time
	ActorKind string // "user", "agent", "guest" or "system"
	ActorID   string // user id; for an agent, the run id
	ActingFor string // agent only: the user the run acts for
	Action    string // module-prefixed action id, e.g. "gate.decide"
	Subject   Subject
	Detail    map[string]string
	PrevHash  []byte // Hash of event Seq-1; empty for the first event
	Hash      []byte // sha256(PrevHash || canonical(event))
}

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrInvalidEvent means a required field (ActorKind, ActorID, Action) is empty.
	ErrInvalidEvent = errors.New("audit: invalid event")
	// ErrChainBroken means Verify found an event whose hash or link does not match.
	ErrChainBroken = errors.New("audit: hash chain broken")
	// ErrBadRange means Verify was called with from > to or from < 1.
	ErrBadRange = errors.New("audit: bad sequence range")
	// ErrImmutable means a caller tried to change or remove an audit row.
	ErrImmutable = errors.New("audit: rows are append-only")
)

// Log is the append-only, hash-chained audit trail.
type Log interface {
	// Append writes e inside the caller's transaction, chained to the previous event.
	Append(ctx context.Context, tx pgx.Tx, e Event) error
	// Verify recomputes the chain from..to (inclusive) and returns ErrChainBroken on the
	// first mismatch.
	Verify(ctx context.Context, from, to Seq) error
}

// Masker strips values that may not reach audit detail (document text, customer names,
// secrets) before Append stores it.
type Masker interface {
	Mask(detail map[string]string) map[string]string
}
