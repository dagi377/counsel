// CONTRACT: kernel/work. The shape of this file is frozen after wave 0; changes are additive
// only and need a docs/QUESTIONS.md entry. Bodies live in the T-034 subtasks.

package work

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dagi377/counsel/kernel/registry"
	"time"

	"github.com/jackc/pgx/v5"
)

// Sentinel errors.
var (
	ErrNotFound = errors.New("work: not found")
	// ErrInvalidItem: missing title, type, workflow, status or reporter, or priority outside 1..5.
	ErrInvalidItem = errors.New("work: invalid item")
	// ErrTransitionNotAllowed: the workflow has no transition between the two statuses.
	ErrTransitionNotAllowed = errors.New("work: transition not allowed")
	// ErrSelfApproval: the approver is the item's assignee (preparer), or an agent.
	ErrSelfApproval = errors.New("work: preparer or agent cannot approve")
	// ErrEffectNeedsGate: an automation action with an effect names no gate kind.
	ErrEffectNeedsGate = errors.New("work: automation effect must open a gate")
	// ErrStale: the caller's version is not the current one.
	ErrStale = errors.New("work: stale version")
)

// ItemID identifies a work item.
type ItemID string

// StatusGroup is the coarse state every workflow status maps to.
type StatusGroup string

// Status groups.
const (
	GroupTodo       StatusGroup = "todo"
	GroupInProgress StatusGroup = "in_progress"
	GroupApproval   StatusGroup = "approval"
	GroupDone       StatusGroup = "done"
)

// Subject links an item to a record owned by another module (registered subject kind).
type Subject = registry.Subject

// Item is one work item. Type is a registered item type (for example "task" or "review").
type Item struct {
	ID    ItemID
	Key   string
	Type  string
	Title string
	// Description is the item's Markdown body, or nil for none (work_item.description,
	// nullable). Additive; see docs/QUESTIONS.md TOOL16-DESC.
	Description     *string
	WorkflowID      string
	StatusID        string
	Subject         Subject
	ParentID        ItemID
	AssigneeUserID  string
	AssigneeIsAgent bool
	ApproverID      string
	ReporterID      string
	Priority        int // 1 (highest) .. 5; 0 means the default, 3
	DueAt           *time.Time
	Labels          []string
	EstimateMin     int
	Version         int
}

// Status is one workflow status.
type Status struct {
	ID           string
	WorkflowID   string
	Name         string
	Group        StatusGroup
	IsAgentReady bool
}

// Transition is an allowed move between two statuses of one workflow.
type Transition struct {
	WorkflowID string
	From, To   string
	Name       string
}

// Filter selects items for List. Empty fields match everything.
type Filter struct {
	Subject        Subject
	AssigneeUserID string
	StatusID       string
	Limit          int
	// After is a page cursor: only items after this one in id order. Empty starts at the
	// first item. Added for the paged work-item list (docs/QUESTIONS.md HTTP-23).
	After ItemID
}

// Expr is an automation condition, stored as JSON.
type Expr = json.RawMessage

// ActionRef names a registered automation action. An action with an effect must name the
// gate kind it opens; it never applies the effect directly.
type ActionRef struct {
	Name      string
	Params    json.RawMessage
	HasEffect bool
	GateKind  string
}

// Automation runs Action when Trigger fires and Cond holds.
type Automation struct {
	ID      string
	Trigger string
	Cond    Expr
	Action  ActionRef
	Enabled bool
}

// Service creates and moves work items. Writes take the caller's transaction.
type Service interface {
	Create(ctx context.Context, tx pgx.Tx, it Item) (ItemID, error)
	Get(ctx context.Context, id ItemID) (Item, error)
	List(ctx context.Context, f Filter) ([]Item, error)
	// Move changes the status along a workflow transition. version is the caller's Item.Version.
	Move(ctx context.Context, tx pgx.Tx, id ItemID, version int, toStatusID string, by string) error
	// SetApprover sets the approver; the assignee and agents are refused with ErrSelfApproval.
	SetApprover(ctx context.Context, tx pgx.Tx, id ItemID, approverID string, approverIsAgent bool) error
}

// Automations stores automation rules.
type Automations interface {
	// Save validates and stores a; an effect without a gate kind is ErrEffectNeedsGate.
	Save(ctx context.Context, tx pgx.Tx, a Automation) (string, error)
	ForTrigger(ctx context.Context, trigger string) ([]Automation, error)
}
