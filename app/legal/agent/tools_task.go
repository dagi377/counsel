package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/work"
)

// ToolCreateTask proposes a work item. It starts in the task workflow's proposed status and
// becomes work only when its assignee or approver accepts it. When the agent run's feature
// declares a gate kind on work items (WorkItemGateKinds), the item is returned as a draft of
// the run instead, so submit_for_approval can open that gate on it.
const ToolCreateTask = "create_task"

// WorkItemGateKinds are the gate kinds whose subject is a work item an agent drafts: the
// cover task of an absence (work.absence) and a work output (work.output).
var WorkItemGateKinds = []string{"work.absence", "work.output"}

// WorkTypeTask is the work item type create_task creates.
const WorkTypeTask = "task"

// TaskCreator creates work items (kernel/work Service.Create).
type TaskCreator interface {
	Create(ctx context.Context, tx pgx.Tx, it work.Item) (work.ItemID, error)
}

// ProposedTasks names where an agent-proposed task starts: the task workflow and its
// proposed status (work configuration, wired at boot).
type ProposedTasks interface {
	ProposedTaskStatus(ctx context.Context) (workflowID, statusID string, err error)
}

// CreateTaskHandler is create_task's handler, for the boot HandlerSet.
func CreateTaskHandler() Handler {
	return Handler{Name: ToolCreateTask, Build: buildCreateTask}
}

// buildCreateTask reads Deps: CapabilityCheck, roles.Walls, TaskCreator and ProposedTasks.
func buildCreateTask(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolCreateTask, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolCreateTask, err)
	}
	items, err := DepOf[TaskCreator](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolCreateTask, err)
	}
	proposed, err := DepOf[ProposedTasks](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolCreateTask, err)
	}
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: zone: %w", ToolCreateTask, err)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: ToolCreateTask, Kind: Write}})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolCreateTask, err)
	}
	s := specs[0]
	t := createTask{can: can, walls: walls, items: items, proposed: proposed, capability: s.Capability, loc: loc}
	// rule: ARC-I-035
	// WriteTool holds every result to a draft, a proposal or an opened gate.
	tool, err := agenttools.WriteTool(registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema, Handle: t.handle,
	})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolCreateTask, err)
	}
	return tool, nil
}

type createTask struct {
	can        CapabilityCheck
	walls      roles.Walls
	items      TaskCreator
	proposed   ProposedTasks
	capability string
	loc        *time.Location
}

type createTaskIn struct {
	MatterID   string `json:"matter_id"`
	Title      string `json:"title"`
	AssigneeID string `json:"assignee_id"`
	DueOn      string `json:"due_on"`
	// Description is the task's Markdown body; it is stored on the work item.
	Description string `json:"description_markdown"`
}

func (t createTask) handle(ctx context.Context, p registry.Principal, raw json.RawMessage) (registry.ToolResult, error) {
	user := actor(p)
	// rule: ARC-I-036
	ok, err := t.can(ctx, user, t.capability)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: capability: %w", ToolCreateTask, err)
	}
	if !ok {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolCreateTask, ErrForbidden)
	}
	var in createTaskIn
	if err := json.Unmarshal(raw, &in); err != nil || in.Title == "" {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolCreateTask, ErrInvalidInput)
	}
	it := work.Item{Type: WorkTypeTask, Title: in.Title, ReporterID: user, AssigneeUserID: in.AssigneeID}
	if in.Description != "" {
		d := in.Description
		it.Description = &d
	}
	if in.MatterID != "" {
		// rule: ARC-I-036
		if err := visibleTo(ctx, t.walls, user, in.MatterID); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolCreateTask, err)
		}
		// The assignee must see the matter too: work on a walled matter is never handed to a
		// walled user.
		if in.AssigneeID != "" {
			if err := visibleTo(ctx, t.walls, in.AssigneeID, in.MatterID); err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: assignee: %w", ToolCreateTask, err)
			}
		}
		it.Subject = registry.Subject{Kind: WorkSubjectMatter, ID: in.MatterID}
	}
	if in.DueOn != "" {
		// A due date is a calendar day in the business zone, held as its midnight there.
		due, err := time.ParseInLocation(time.DateOnly, in.DueOn, t.loc)
		if err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: due date: %w", ToolCreateTask, ErrInvalidInput)
		}
		it.DueAt = &due
	}
	it.WorkflowID, it.StatusID, err = t.proposed.ProposedTaskStatus(ctx)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: proposed status: %w", ToolCreateTask, err)
	}
	// rule: ARC-I-035
	// The item is created proposed, with no approver: the agent never accepts it, and only the
	// assignee or an approver moves it on.
	id, err := t.items.Create(ctx, nil, it)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolCreateTask, err)
	}
	outcome := agenttools.OutcomeProposal
	if draftsWorkItems(ctx, p) {
		// rule: AI-R-003
		// A draft of the run: only submit_for_approval moves it on, and the gate it opens is
		// decided by a person other than the acting user, never by the agent.
		outcome = agenttools.OutcomeDraft
	}
	out, err := json.Marshal(agenttools.WriteOutput{Outcome: outcome, Ref: string(id)})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolCreateTask, err)
	}
	return registry.ToolResult{Output: out}, nil
}

// draftsWorkItems reports whether p is an agent whose run scope declares a gate kind on work
// items. A user principal, or an agent with no such kind, gets a proposal.
func draftsWorkItems(ctx context.Context, p registry.Principal) bool {
	if p.Kind != registry.PrincipalAgent {
		return false
	}
	s, ok := agenttools.ScopeOf(ctx)
	if !ok || s.RunID != p.RunID {
		return false
	}
	for _, k := range WorkItemGateKinds {
		if s.AllowsGateKind(k) {
			return true
		}
	}
	return false
}
