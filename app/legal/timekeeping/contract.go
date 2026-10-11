// CONTRACT: app/legal/timekeeping (T-016). Frozen once on main; changes are additive only and
// need a docs/QUESTIONS.md entry. Source: spec/eng/b3-billing-engine.json (state_machines
// TimeEntry, rules BIL-C-013, BIL-C-014, BIL-I-007, BIL-L-046) and the timekeeping.* tables
// in migrations/0001_init.sql. Bodies live in the T-016 subtasks (entry.go, timer.go,
// writedown.go, paralegal.go).

package timekeeping

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// EntryID identifies a time entry (timekeeping.time_entry.id).
type EntryID string

// TimerID identifies a timer (timekeeping.timer.id).
type TimerID string

// State is a TimeEntry state from the b3 TimeEntry state machine.
type State string

// TimeEntry states.
const (
	StateSuggested   State = "suggested"
	StateDraft       State = "draft"
	StateAccepted    State = "accepted"
	StateSubmitted   State = "submitted"
	StateApproved    State = "approved"
	StateOnDraftBill State = "on_draft_bill"
	StateBilled      State = "billed"
	StateWrittenOff  State = "written_off"
	StateRejected    State = "rejected"
)

// Origin says how an entry was created (time_entry.origin).
type Origin string

// Origins.
const (
	OriginManual     Origin = "manual"
	OriginTimer      Origin = "timer"
	OriginSuggestion Origin = "suggestion"
)

// Actor is who performs a transition. An agent (Agent true) only suggests: it never accepts,
// approves or writes down; UserID is then the user it acts for.
type Actor struct {
	UserID string
	Agent  bool
}

// TimeEntry is the accepted record of work. Minutes never change after acceptance; a
// write-down changes the bill line amount only (BIL-C-013).
type TimeEntry struct {
	ID            EntryID
	MatterID      string
	UserID        string // the timekeeper who owns the entry
	WorkDate      time.Time
	Minutes       int
	BilledMinutes int
	ActivityCode  string
	Narrative     string
	Billable      bool
	Origin        Origin
	State         State
	BillID        string // set while the entry is on a non-void bill
	Flags         []string
}

// TimerState is the state of a running timer.
type TimerState string

// Timer states.
const (
	TimerRunning TimerState = "running"
	TimerPaused  TimerState = "paused"
	TimerStopped TimerState = "stopped"
)

// Timer measures raw minutes for one user and matter. At most one runs per user.
type Timer struct {
	ID            TimerID
	UserID        string
	MatterID      string
	StartedAt     time.Time
	PausedMinutes int
	State         TimerState
}

// WriteDown adjusts the billed amount of an entry (timekeeping.write_down).
type WriteDown struct {
	EntryID       EntryID
	OriginalCents int64
	AdjustedCents int64
	Kind          string // "write_down" or "write_up"
	ReasonCode    string
	Stage         string // "pre_bill" or "post_bill"
	ProposedBy    Actor
}

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrNotFound means the entry or timer does not exist.
	ErrNotFound = errors.New("timekeeping: not found")
	// ErrInvalid means a required field is missing or out of range.
	ErrInvalid = errors.New("timekeeping: invalid")
	// ErrBadTransition means the TimeEntry state machine has no such transition.
	ErrBadTransition = errors.New("timekeeping: transition not allowed")
	// ErrNotOwner means only the entry owner may accept a suggestion.
	ErrNotOwner = errors.New("timekeeping: not the entry owner")
	// ErrAgentForbidden means an agent tried to accept, approve or write down.
	ErrAgentForbidden = errors.New("timekeeping: agent may only suggest")
	// ErrSelfApproval means the timekeeper tried to approve their own timesheet.
	ErrSelfApproval = errors.New("timekeeping: preparer cannot approve")
	// ErrTimerRunning means the user already has a running timer.
	ErrTimerRunning = errors.New("timekeeping: a timer is already running")
	// ErrTimerStopped means the timer is not running.
	ErrTimerStopped = errors.New("timekeeping: timer is stopped")
	// ErrBilledLocked means the entry is on a non-void bill and cannot be edited (409).
	ErrBilledLocked = errors.New("timekeeping: entry is on a bill")
	// ErrOutOfScope means a paralegal entry is outside the paralegal scope list; the entry is
	// flagged and blocked from billing.
	ErrOutOfScope = errors.New("timekeeping: activity outside paralegal scope")
)

// Entries records time and moves entries through the TimeEntry state machine.
type Entries interface {
	// Record saves a human-authored entry; it is accepted at once (draft -> accepted).
	Record(ctx context.Context, tx pgx.Tx, e TimeEntry, by Actor) (EntryID, error)
	// Suggest saves an agent or signal suggestion in state suggested. It is never
	// auto-accepted.
	Suggest(ctx context.Context, tx pgx.Tx, e TimeEntry, by Actor) (EntryID, error)
	// Transition moves id to state to, checking the state machine and who may act.
	Transition(ctx context.Context, tx pgx.Tx, id EntryID, to State, by Actor) error
	// Edit changes minutes, narrative or activity of an entry not on a bill.
	Edit(ctx context.Context, tx pgx.Tx, e TimeEntry, by Actor) error
	// Get returns one entry.
	Get(ctx context.Context, id EntryID) (TimeEntry, error)
}

// Timers runs per-user timers. Stop returns raw minutes; rounding happens at billing.
type Timers interface {
	Start(ctx context.Context, tx pgx.Tx, userID, matterID string) (TimerID, error)
	Stop(ctx context.Context, tx pgx.Tx, id TimerID) (minutes int, err error)
}

// WriteDowns records write-downs and write-ups against an entry's bill line amount.
type WriteDowns interface {
	WriteDown(ctx context.Context, tx pgx.Tx, w WriteDown) error
}

// ScopeChecker checks a paralegal timekeeper's activity against the paralegal scope list.
type ScopeChecker interface {
	CheckScope(ctx context.Context, userID, activityCode string) error
}
