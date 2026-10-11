package agenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolSubmitForApproval opens the gate for a draft the agent created in this run.
const ToolSubmitForApproval = "submit_for_approval"

// CapAgentAsk is the capability submit_for_approval needs (a2-interfaces).
const CapAgentAsk = "agent.ask"

// ErrGateKindNotAllowed: an agent run tried to open a gate kind its feature does not declare
// (403 gate_kind_not_allowed).
var ErrGateKindNotAllowed = errors.New("agenttools: gate_kind_not_allowed")

// ErrNoRunScope: an agent tool ran without its run's scope in the context.
var ErrNoRunScope = errors.New("agenttools: no run scope")

// Scope is what one agent run may touch: the gate kinds its feature declares, the drafts
// the run itself created and the citations its tool results returned. The Runner stores one
// Scope per run in the context; tools read it with ScopeOf. Drafts and the citations grow as
// the run's tools return results. RunScopes is the same scope seen by run id.
type Scope struct {
	RunID     string
	GateKinds []string
	Drafts    []string // refs (subject ids) of drafts created in this run

	mu    sync.Mutex
	cited []Citation // citations the run's tool results returned
}

// NewScope returns the scope of run runID that may open gateKinds.
func NewScope(runID string, gateKinds []string) *Scope {
	return &Scope{RunID: runID, GateKinds: slices.Clone(gateKinds)}
}

type scopeKey struct{}

// WithScope returns ctx carrying s.
func WithScope(ctx context.Context, s *Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, s)
}

// ScopeOf returns the run scope in ctx, if any.
func ScopeOf(ctx context.Context) (*Scope, bool) {
	s, ok := ctx.Value(scopeKey{}).(*Scope)
	return s, ok && s != nil
}

// AddDraft records a draft the run created.
func (s *Scope) AddDraft(ref string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ref != "" && !slices.Contains(s.Drafts, ref) {
		s.Drafts = append(s.Drafts, ref)
	}
}

// HasDraft reports whether the run created the draft ref.
func (s *Scope) HasDraft(ref string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.Drafts, ref)
}

// AddCitations records citations a tool result of the run returned.
func (s *Scope) AddCitations(cs []Citation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range cs {
		if !slices.Contains(s.cited, c) {
			s.cited = append(s.cited, c)
		}
	}
}

// Cites reports whether a citation the run was returned covers c: the same document version
// and page, with c's lines inside the returned range.
func (s *Scope) Cites(c Citation) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.cited {
		if r.VersionID == c.VersionID && r.Page == c.Page && r.FromLine <= c.FromLine && c.ToLine <= r.ToLine {
			return true
		}
	}
	return false
}

// AllowsGateKind reports whether the run's feature declares kind.
func (s *Scope) AllowsGateKind(kind string) bool {
	return slices.Contains(s.GateKinds, kind)
}

// agentScope returns the scope an agent principal acts in. It fails closed: an agent with no
// scope for its run in ctx may open no gate. A user principal (the manual path) has no run
// scope and gets nil, nil.
func agentScope(ctx context.Context, p Principal) (*Scope, error) {
	if p.Kind != registry.PrincipalAgent {
		return nil, nil
	}
	s, ok := ScopeOf(ctx)
	if !ok || s.RunID != p.RunID {
		return nil, fmt.Errorf("run %s: no run scope: %w", p.RunID, ErrGateKindNotAllowed)
	}
	return s, nil
}

// CheckGateKind returns ErrGateKindNotAllowed when an agent principal opens a gate kind
// outside its run scope. User principals pass.
func CheckGateKind(ctx context.Context, p Principal, kind string) error {
	s, err := agentScope(ctx, p)
	if err != nil || s == nil {
		return err
	}
	// rule: ARC-I-035
	if !s.AllowsGateKind(kind) {
		return fmt.Errorf("gate kind %q: %w", kind, ErrGateKindNotAllowed)
	}
	return nil
}

// RecordDraft records res in the run scope when an agent's write tool returned a draft.
func RecordDraft(ctx context.Context, p Principal, res ToolResult) {
	if p.Kind != registry.PrincipalAgent {
		return
	}
	s, ok := ScopeOf(ctx)
	if !ok || s.RunID != p.RunID {
		return
	}
	var out WriteOutput
	if json.Unmarshal(res.Output, &out) == nil && out.Outcome == OutcomeDraft && res.GateID == "" {
		s.AddDraft(out.Ref)
	}
}

// RecordCitations records res's citations in the run scope when an agent's tool returned
// them, so a later write tool may cite only what the run was shown.
func RecordCitations(ctx context.Context, p Principal, res ToolResult) {
	if p.Kind != registry.PrincipalAgent || len(res.Citations) == 0 {
		return
	}
	s, ok := ScopeOf(ctx)
	if !ok || s.RunID != p.RunID {
		return
	}
	s.AddCitations(res.Citations)
}

// RunScopes is the run scope in ctx seen by run id, for tools that take the scope as a
// dependency (app/legal/agent RunScope). It holds no state: everything lives in the Scope the
// Runner put in ctx, and a ctx without the run's scope fails closed.
type RunScopes struct{}

// Returned reports whether a tool result of run runID returned a citation covering c.
func (RunScopes) Returned(ctx context.Context, runID string, c Citation) (bool, error) {
	s, ok := ScopeOf(ctx)
	if !ok || s.RunID != runID {
		return false, fmt.Errorf("run %s: %w", runID, ErrNoRunScope)
	}
	return s.Cites(c), nil
}

// RecordDraft records draft ref as made in run runID.
func (RunScopes) RecordDraft(ctx context.Context, runID, ref string) error {
	s, ok := ScopeOf(ctx)
	if !ok || s.RunID != runID {
		return fmt.Errorf("run %s: %w", runID, ErrNoRunScope)
	}
	s.AddDraft(ref)
	return nil
}

// ScopedOpener wraps open so every gate it opens for an agent principal is checked against
// the run scope first: a kind outside Scope.GateKinds is refused before any gate exists.
func ScopedOpener(open GateOpener) GateOpener {
	return func(ctx context.Context, kind string, subject registry.Subject, payload json.RawMessage, preparer Principal) (gate.GateID, error) {
		// rule: AI-R-003
		// The agent opens only its feature's gate kinds; it never decides one.
		if err := CheckGateKind(ctx, preparer, kind); err != nil {
			return "", err
		}
		return open(ctx, kind, subject, payload, preparer)
	}
}

// submitSchema is the submit_for_approval input (a2-interfaces). There is no field for a
// decision: the tool opens a gate and nothing else.
const submitSchema = `{
  "type": "object",
  "required": ["subject_type", "subject_id", "gate_kind"],
  "additionalProperties": false,
  "properties": {
    "subject_type": {"type": "string", "minLength": 1},
    "subject_id": {"type": "string", "minLength": 1},
    "gate_kind": {"type": "string", "minLength": 1},
    "note": {"type": "string", "maxLength": 1000}
  }
}`

type submitInput struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	GateKind    string `json:"gate_kind"`
}

// SubmitForApprovalTool returns the submit_for_approval write tool. For an agent principal
// the gate kind must be in the run scope (ErrGateKindNotAllowed, 403) and the subject must be
// a draft this run created (ErrNotFound, 404). The gate payload carries the subject ids only;
// the model's note is not stored (AI-R-005). The caller is the gate's preparer, so the agent
// never approves.
func SubmitForApprovalTool(open GateOpener) (Tool, error) {
	if open == nil {
		return Tool{}, fmt.Errorf("write tool %q: opener required: %w", ToolSubmitForApproval, ErrBadTool)
	}
	return WriteTool(Tool{
		Name:       ToolSubmitForApproval,
		Kind:       registry.ToolWrite,
		Input:      []byte(submitSchema),
		Capability: CapAgentAsk,
		Handle: func(ctx context.Context, p Principal, raw json.RawMessage) (ToolResult, error) {
			var in submitInput
			if err := json.Unmarshal(raw, &in); err != nil {
				return ToolResult{}, fmt.Errorf("submit for approval: %w", ErrInvalidInput)
			}
			// rule: ARC-I-035
			if err := CheckGateKind(ctx, p, in.GateKind); err != nil {
				return ToolResult{}, fmt.Errorf("submit for approval: %w", err)
			}
			// rule: AI-R-003
			// An agent submits only drafts it created in this run.
			if s, err := agentScope(ctx, p); err != nil {
				return ToolResult{}, fmt.Errorf("submit for approval: %w", err)
			} else if s != nil && !s.HasDraft(in.SubjectID) {
				return ToolResult{}, fmt.Errorf("submit for approval: draft %s: %w", in.SubjectID, ErrNotFound)
			}
			payload, err := json.Marshal(map[string]string{"subject_type": in.SubjectType, "subject_id": in.SubjectID})
			if err != nil {
				return ToolResult{}, fmt.Errorf("submit for approval: %w", err)
			}
			subject := registry.Subject{Kind: in.SubjectType, ID: in.SubjectID}
			id, err := open(ctx, in.GateKind, subject, payload, p)
			if err != nil {
				return ToolResult{}, fmt.Errorf("submit for approval %q: %w", in.GateKind, err)
			}
			out, err := json.Marshal(WriteOutput{Outcome: OutcomeGate, Ref: string(id)})
			if err != nil {
				return ToolResult{}, fmt.Errorf("submit for approval: %w", err)
			}
			return ToolResult{Output: out, GateID: string(id)}, nil
		},
	})
}
