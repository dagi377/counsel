package agenttools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// WriteOutcome is the only kind of change a write tool may make: it drafts, it proposes, or
// it opens a gate. It never sends, approves or executes anything itself.
type WriteOutcome string

// Write outcomes.
const (
	OutcomeDraft    WriteOutcome = "draft"
	OutcomeProposal WriteOutcome = "proposal"
	OutcomeGate     WriteOutcome = "gate"
)

// WriteOutput is the Output a write tool returns: what it created and its id.
type WriteOutput struct {
	Outcome WriteOutcome `json:"outcome"`
	Ref     string       `json:"ref"`
}

// CheckWriteResult returns nil when res is a draft, a proposal or an opened gate. A gate
// outcome must carry the GateID and Ref must name that gate. Anything else breaks the tool's
// definition and is ErrBadTool.
func CheckWriteResult(res ToolResult) error {
	var out WriteOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		return fmt.Errorf("write result: output is not a write outcome: %w", ErrBadTool)
	}
	if out.Ref == "" {
		return fmt.Errorf("write result: empty ref: %w", ErrBadTool)
	}
	switch out.Outcome {
	case OutcomeDraft, OutcomeProposal:
		if res.GateID != "" {
			return fmt.Errorf("write result: %s with a gate id: %w", out.Outcome, ErrBadTool)
		}
	case OutcomeGate:
		if res.GateID == "" || res.GateID != out.Ref {
			return fmt.Errorf("write result: gate outcome must carry its gate id: %w", ErrBadTool)
		}
	default:
		return fmt.Errorf("write result: outcome %q: %w", out.Outcome, ErrBadTool)
	}
	return nil
}

// WriteTool wraps a write tool's handler so every result is checked with CheckWriteResult:
// a handler that returns anything but a draft, a proposal or an opened gate fails the call.
// It returns ErrBadTool when t is not a write tool.
func WriteTool(t Tool) (Tool, error) {
	if t.Kind != registry.ToolWrite || t.Handle == nil {
		return Tool{}, fmt.Errorf("write tool %q: %w", t.Name, ErrBadTool)
	}
	h := t.Handle
	// rule: ARC-I-035
	t.Handle = func(ctx context.Context, p Principal, in json.RawMessage) (ToolResult, error) {
		res, err := h(ctx, p, in)
		if err != nil {
			return ToolResult{}, err
		}
		if err := CheckWriteResult(res); err != nil {
			return ToolResult{}, err
		}
		return res, nil
	}
	return t, nil
}

// GateOpener opens a gate inside the caller's transaction; the caller binds the tx, for
// example func(ctx, ...) { return engine.Open(ctx, tx, ...) }.
type GateOpener func(ctx context.Context, kind string, subject registry.Subject, payload json.RawMessage, preparer Principal) (gate.GateID, error)

// openGateSchema is the open_gate input. There is no field for a decision: the tool opens a
// gate and nothing else.
const openGateSchema = `{
  "type": "object",
  "required": ["kind", "subject", "payload"],
  "additionalProperties": false,
  "properties": {
    "kind": {"type": "string", "minLength": 1},
    "subject": {
      "type": "object",
      "required": ["kind", "id"],
      "additionalProperties": false,
      "properties": {"kind": {"type": "string", "minLength": 1}, "id": {"type": "string", "minLength": 1}}
    },
    "payload": {"type": "object"}
  }
}`

type openGateInput struct {
	Kind    string `json:"kind"`
	Subject struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"subject"`
	Payload json.RawMessage `json:"payload"`
}

// OpenGateTool returns the kernel's open_gate write tool. The calling principal is the
// gate's preparer (for an agent, its acting-for user counts as the preparer), so the agent
// can never be the one who approves. capability may be empty.
func OpenGateTool(open GateOpener, capability string) (Tool, error) {
	if open == nil {
		return Tool{}, fmt.Errorf("write tool %q: opener required: %w", ToolOpenGate, ErrBadTool)
	}
	return WriteTool(Tool{
		Name:       ToolOpenGate,
		Kind:       registry.ToolWrite,
		Input:      []byte(openGateSchema),
		Capability: capability,
		Handle: func(ctx context.Context, p Principal, raw json.RawMessage) (ToolResult, error) {
			var in openGateInput
			if err := json.Unmarshal(raw, &in); err != nil {
				return ToolResult{}, fmt.Errorf("open gate: %w", ErrInvalidInput)
			}
			subject := registry.Subject{Kind: in.Subject.Kind, ID: in.Subject.ID}
			id, err := open(ctx, in.Kind, subject, in.Payload, p)
			if err != nil {
				return ToolResult{}, fmt.Errorf("open gate %q: %w", in.Kind, err)
			}
			out, err := json.Marshal(WriteOutput{Outcome: OutcomeGate, Ref: string(id)})
			if err != nil {
				return ToolResult{}, fmt.Errorf("open gate: %w", err)
			}
			return ToolResult{Output: out, GateID: string(id)}, nil
		},
	})
}

// DecideGuard wraps a gate engine so only a user principal can decide a gate. An agent
// principal deciding any gate gets ErrAgentDecides (403 agent_cannot_decide), whatever the
// gate kind, verdict or the agent's grants; so does any other non-user principal. Every
// other method passes through.
type DecideGuard struct {
	gate.Engine
}

var _ gate.Engine = DecideGuard{}

// Decide implements gate.Engine.
func (g DecideGuard) Decide(ctx context.Context, tx pgx.Tx, id gate.GateID, version int, d gate.Decision, by Principal) error {
	// rule: ARC-I-017
	if by.Kind != registry.PrincipalUser {
		return fmt.Errorf("decide %s: %w", id, ErrAgentDecides)
	}
	if err := g.Engine.Decide(ctx, tx, id, version, d, by); err != nil {
		return fmt.Errorf("decide %s: %w", id, err)
	}
	return nil
}
