// CONTRACT: kernel/retention. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-032 subtasks (class.go, purge.go).
// Source: spec/eng/k1-kernel.html (retention.Propose).
//
// A purge run only proposes; nothing is destroyed until the class's destruction gate is
// approved, and then only inside that gate's effect handler.

package retention

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Class is a retention class the app registers at boot.
type Class struct {
	ID              string
	MinYears        int
	NeverPurge      bool
	DestructionGate string // gate kind slug that approves a purge; required unless NeverPurge
}

// Record is one retained record as the engine sees it. ClosedOn is the calendar day
// (America/Toronto) the retention clock started.
type Record struct {
	ID       string
	Class    string
	Kind     string // opaque subject kind
	ClosedOn time.Time
	Hold     bool // a hold blocks proposal
}

// PurgeList is a proposal to destroy records of one class. It is applied only by the
// destruction gate's effect.
type PurgeList struct {
	ID      string
	Class   string
	AsOf    time.Time
	Records []string // Record ids
}

// Sentinel errors.
var (
	ErrUnknownClass   = errors.New("retention: unknown class")
	ErrDuplicateClass = errors.New("retention: class already registered")
	ErrInvalidClass   = errors.New("retention: invalid class")
	ErrNeverPurge     = errors.New("retention: class is never purged")
	ErrUnknownList    = errors.New("retention: unknown purge list")
	ErrNotApproved    = errors.New("retention: purge list not approved by its destruction gate")
)

// Classes holds the registered retention classes (T-032.4).
type Classes interface {
	RegisterClass(c Class) error
	Class(id string) (Class, error)
}

// Engine proposes purges and applies approved ones (T-032.5).
type Engine interface {
	// Propose lists the records of a class past their minimum retention on asOf. It
	// destroys nothing.
	Propose(ctx context.Context, class string) (PurgeList, error)
	// Apply destroys the records of an approved purge list. It runs only inside the
	// destruction gate's effect handler, in that handler's tx; gateID names the approved gate.
	Apply(ctx context.Context, tx pgx.Tx, listID, gateID string) error
}

// Service is everything kernel/retention provides.
type Service interface {
	Classes
	Engine
}
