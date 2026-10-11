// CONTRACT: kernel/deadline (T-031). Frozen once on main; changes are additive only and need
// a docs/QUESTIONS.md entry. Source: spec/eng/k1-kernel.html ("kernel/deadline": Compute,
// Confirm; rule packs are versioned YAML validated by rulepack.schema.json). Bodies live in
// the T-031 subtasks (pack.go, compute.go, confirm.go, remind.go).

package deadline

import (
	"context"
	"errors"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/registry"

	"github.com/jackc/pgx/v5"
)

// Date is a civil date in the business time zone.
type Date = clock.Date

// Subject is an opaque reference to a business object (registry.Subject shape).
type Subject = registry.Subject

// Principal is who acts (registry.Principal shape). Kind "agent" never confirms.
type Principal = registry.Principal

// DeadlineID identifies a stored deadline.
type DeadlineID string

// DayCount is how a rule counts days.
type DayCount string

// Day-count modes.
const (
	// Calendar counts every day: trigger + N.
	Calendar DayCount = "calendar"
	// Clear excludes both the trigger day and the due day: trigger + N + 1.
	Clear DayCount = "clear"
	// Business counts only weekdays that are not holidays.
	Business DayCount = "business"
)

// Roll says what happens when a computed date is not a business day.
type Roll string

// Roll rules.
const (
	RollNone    Roll = "none"
	RollForward Roll = "forward" // move to the next business day
)

// Rule is one deadline rule inside a pack.
type Rule struct {
	ID          string
	Trigger     string // trigger event id
	Mode        DayCount
	Days        int // offset in days (may be 0 when MonthsAfter is set)
	MonthsAfter int // added before Days
	Roll        Roll
}

// Pack is a versioned rule pack loaded from YAML.
type Pack struct {
	ID       string
	Version  string
	Rules    []Rule
	Holidays []Date
}

// Proposed is a computed deadline awaiting human confirmation.
type Proposed struct {
	PackID      string
	PackVersion string
	RuleID      string
	Trigger     Date
	Due         Date
	Subject     Subject
}

// State is the confirmation state of a stored deadline.
type State string

// States.
const (
	StateProposed  State = "proposed"
	StateConfirmed State = "confirmed"
)

// Deadline is a stored proposal and its confirmation.
type Deadline struct {
	ID          DeadlineID
	Proposed    Proposed
	State       State
	ConfirmedBy string // user id
}

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrUnknownPack means no pack with that id is loaded.
	ErrUnknownPack = errors.New("deadline: unknown rule pack")
	// ErrUnknownRule means the pack has no rule with that id.
	ErrUnknownRule = errors.New("deadline: unknown rule")
	// ErrInvalidPack means a pack failed schema validation.
	ErrInvalidPack = errors.New("deadline: invalid rule pack")
	// ErrNotFound means the deadline does not exist.
	ErrNotFound = errors.New("deadline: not found")
	// ErrAlreadyConfirmed means the deadline is already confirmed.
	ErrAlreadyConfirmed = errors.New("deadline: already confirmed")
	// ErrAgentCannotConfirm means an agent principal tried to confirm; only a person confirms.
	ErrAgentCannotConfirm = errors.New("deadline: agent cannot confirm")
)

// Engine computes deadlines from rule packs; every computed deadline needs human confirmation.
type Engine interface {
	// LoadPack validates and registers a pack version.
	LoadPack(ctx context.Context, p Pack) error
	// Compute returns the proposals for rule of pack, triggered on trigger.
	Compute(ctx context.Context, pack, rule string, trigger Date, subject Subject) ([]Proposed, error)
	// Save stores a proposal in state proposed.
	Save(ctx context.Context, tx pgx.Tx, p Proposed) (DeadlineID, error)
	// Confirm records a person's confirmation (append-only).
	Confirm(ctx context.Context, tx pgx.Tx, id DeadlineID, by Principal) error
	// Get returns one stored deadline.
	Get(ctx context.Context, id DeadlineID) (Deadline, error)
}
