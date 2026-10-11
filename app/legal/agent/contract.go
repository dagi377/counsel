// CONTRACT: app/legal/agent. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-037L subtasks (tools_read.go,
// tools_write.go, schemas/). Source: spec/eng/a2-interfaces.json (agent tools),
// spec/eng/k1-kernel.html (kernel/agenttools).
//
// The agent only reads, reports and proposes. A write tool opens a Counsel gate and
// returns; nothing is executed until a person other than the acting-for user approves the
// gate, and the run resumes only on Counsel's gate.decided event.

package agent

import (
	"context"
	"encoding/json"
	"errors"
)

// ToolKind says what a tool may do.
type ToolKind string

// Tool kinds. A write tool only drafts or opens a gate.
const (
	Read   ToolKind = "read"
	Report ToolKind = "report"
	Write  ToolKind = "write"
)

// ToolSpec describes one Counsel agent tool.
type ToolSpec struct {
	Name        string
	Kind        ToolKind
	Capability  string          // capability the acting-for user must hold
	InputSchema json.RawMessage // JSON Schema for Call.Input
	GateKind    string          // write tools only: the gate kind the proposal opens
}

// Call is one tool call by an agent run.
type Call struct {
	RunID     string
	ActingFor string // the user the run acts for; counts as the preparer
	Tool      string
	Input     json.RawMessage
}

// Citation points at lines of a stored document version.
type Citation struct {
	VersionID string
	Page      int
	FromLine  int
	ToLine    int
}

// Result is what a tool returns. A result that quotes a document carries a citation; a
// write tool sets GateID and leaves the effect unapplied.
type Result struct {
	Output    json.RawMessage
	Citations []Citation
	GateID    string
}

// Decision is a gate outcome.
type Decision string

// Decisions.
const (
	Approved Decision = "approved"
	Rejected Decision = "rejected"
	Expired  Decision = "expired"
)

// GateDecided is Counsel's gate.decided event as the agent sees it.
type GateDecided struct {
	GateID   string
	Decision Decision
	By       string // the deciding user
}

// Sentinel errors.
var (
	ErrUnknownTool  = errors.New("agent: unknown tool")
	ErrInvalidInput = errors.New("agent: tool input fails its schema")
	ErrForbidden    = errors.New("agent: acting-for user lacks the capability")
	ErrNotVisible   = errors.New("agent: subject not visible") // walls: surfaces as not found
	ErrUnknownGate  = errors.New("agent: no run waits on this gate")
	ErrSelfApproval = errors.New("agent: acting-for user cannot decide the run's own gate")
)

// Tools lists and runs the Counsel agent tools (T-037L.1, T-037L.2, T-037L.4).
type Tools interface {
	Specs() []ToolSpec
	Call(ctx context.Context, c Call) (Result, error)
}

// Runs resumes agent runs waiting on Counsel gates (T-037L.2). It returns the run id
// that resumes.
type Runs interface {
	Decided(ctx context.Context, ev GateDecided) (runID string, err error)
}

// Service is everything app/legal/agent provides.
type Service interface {
	Tools
	Runs
}
