// CONTRACT: kernel/agenttools (T-037). Frozen once on main; changes are additive only and need
// a docs/QUESTIONS.md entry. Source: spec/eng/k1-kernel.html ("kernel/agenttools") and
// spec/eng/a2-interfaces.json ARC-I-035, ARC-I-036, ARC-I-038, ARC-I-039. The Tool type and
// its result live in kernel/registry. Bodies live in the T-037 subtasks (tool.go, write.go,
// quote.go, cite.go, plan.go).

package agenttools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dagi377/counsel/kernel/registry"
)

// Tool, its kind, result and citation are the registry types; agenttools does not copy them.
type (
	Tool       = registry.Tool
	ToolKind   = registry.ToolKind
	ToolResult = registry.ToolResult
	Citation   = registry.Citation
	Principal  = registry.Principal
)

// Names of the agent tools the kernel itself provides.
const (
	ToolReportPlan = "report_plan"
	ToolReportStep = "report_step"
	ToolOpenGate   = "open_gate"
	ToolCite       = "cite"
)

// MaxStepTitle is the longest step title, in characters (ARC-I-039).
const MaxStepTitle = 140

// ForbiddenPrefixes are tool name prefixes no agent tool may have (ARC-I-035).
var ForbiddenPrefixes = []string{"send_", "approve_", "authorize_", "execute_"}

// Sentinel errors. Each carries the problem+json code in its comment.
var (
	// ErrPlanRequired: a write tool was called before report_plan (409 plan_required).
	ErrPlanRequired = errors.New("agenttools: plan_required")
	// ErrAgentDecides: an agent principal tried to decide a gate (403).
	ErrAgentDecides = errors.New("agenttools: agent cannot decide a gate")
	// ErrMissingCapability: the principal lacks the tool's capability (403 missing_capability).
	ErrMissingCapability = errors.New("agenttools: missing_capability")
	// ErrUnknownTool: no tool has the name (404).
	ErrUnknownTool = errors.New("agenttools: unknown tool")
	// ErrInvalidInput: the input fails the tool's JSON Schema (422).
	ErrInvalidInput = errors.New("agenttools: input fails schema")
	// ErrBadTool: a tool definition breaks ARC-I-035 (bad kind, forbidden name, duplicate).
	ErrBadTool = errors.New("agenttools: invalid tool definition")
	// ErrStepTitle: a step title is too long or empty (422).
	ErrStepTitle = errors.New("agenttools: invalid step title")
	// ErrNotFound: no plan or step has the id (404).
	ErrNotFound = errors.New("agenttools: not found")
)

// StepStatus is the state of one plan step.
type StepStatus string

// Step statuses.
const (
	StepPending StepStatus = "pending"
	StepRunning StepStatus = "running"
	StepDone    StepStatus = "done"
	StepFailed  StepStatus = "failed"
)

// Step is one step of an agent plan. Title holds no client content beyond a subject reference.
type Step struct {
	Index   int
	Title   string
	Status  StepStatus
	CallIDs []string // tool calls made during the step
	// ID is the step id the agent named in its plan (report_plan step_id), empty when the
	// agent named none. Additive (Q-BOOT-PORTS-B).
	ID string
}

// Plan is the plan an agent run reports before its first write tool.
type Plan struct {
	ID    string
	RunID string
	Steps []Step
}

// Quoted is untrusted text (from a document, email or client) returned to the agent as data,
// never as instructions, with the citation of where it came from (ARC-I-038).
type Quoted struct {
	Untrusted bool     `json:"untrusted"`
	Source    Citation `json:"source"`
	Text      string   `json:"text"`
}

// Tools registers and calls agent tools.
type Tools interface {
	// Register adds a tool; it returns ErrBadTool for a definition that breaks ARC-I-035.
	Register(t Tool) error
	// Call checks capability, input schema and, for write tools, that the run has a plan,
	// then runs the tool. The call is recorded against the run's current step.
	Call(ctx context.Context, p Principal, name string, in json.RawMessage) (ToolResult, error)
}

// Plans keeps agent plans and steps so a run resumes after a crash.
type Plans interface {
	// ReportPlan stores a plan with pending steps for the run, replacing none.
	ReportPlan(ctx context.Context, runID string, titles []string) (Plan, error)
	// ReportStep sets one step's status.
	ReportStep(ctx context.Context, planID string, index int, status StepStatus) error
	// Current returns the run's plan and the index of its first unfinished step (-1 if none).
	Current(ctx context.Context, runID string) (Plan, int, error)
}

// StepIDs names the steps of a stored plan by the ids the agent gave them, so a step id
// survives a restart (Q-BOOT-PORTS-B). Planner implements it.
type StepIDs interface {
	// SetStepIDs sets the id of each step of plan planID, in step order.
	SetStepIDs(ctx context.Context, planID string, ids []string) error
}
