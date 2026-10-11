// CONTRACT: kernel/agenttools/run (FEAT-CORE-01). Frozen once on main; changes are additive
// only and need a docs/QUESTIONS.md entry. Source: spec/eng/a2-interfaces.json ARC-I-035,
// ARC-I-038, ARC-I-039 and spec/ai AI-R-003. Body lives in runner.go.

// Package run drives one feature's model run: it sends the prompt through kernel/ai, runs
// every tool call the model asks for through the agenttools Registry as the run's agent
// principal, and returns what the run produced. It is domain-free: features declare their
// tools, grant and gate kinds in a Spec.
package run

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
)

// Principal is the agent principal a run acts as.
type Principal = agenttools.Principal

// Spec declares one feature's run.
type Spec struct {
	FeatureID    string          // spec feature id; keys fixtures, usage and cost
	Kind         ai.CallKind     // empty means ai.KindToolUse
	Model        string          // logical model alias; empty means "drafting"
	System       string          // system prompt
	Tools        []ai.ToolSpec   // the feature's tools, as the model sees them
	Grant        []string        // capability ids granted to the run's agent principal
	GateKinds    []string        // gate kinds the feature may open
	OutputSchema json.RawMessage // extraction: JSON Schema the final output must satisfy
	// AnswerMayBeUncited lets a chat answer that states no facts from documents carry zero
	// citations (FEAT-CORE-02). Default false: every answer cites this run's tool results.
	AnswerMayBeUncited bool
}

// Trigger is untrusted text that started the run (a message, a document passage). The
// runner quotes it before the first message; it never becomes instructions (ARC-I-038).
type Trigger struct {
	Source agenttools.Citation
	Text   string
}

// Call is one tool call the model made, in order. Refused is the problem code of a call the
// Registry or the tool refused ("plan_required", "missing_capability", ...); empty when it ran.
type Call struct {
	Tool    string
	Refused string
	Err     error // the refusal, for errors.Is; nil when the call ran
}

// Outcome is what one run produced. Text and JSON are returned to the caller only; the run
// stores no model text (AI-R-005).
type Outcome struct {
	Text        string
	JSON        json.RawMessage
	Citations   []agenttools.Citation // every citation the run's tool results carried
	GateIDs     []string              // gates opened by write tools, in order
	RetryQueued bool                  // a transient model fault ended the run; retry later
	Calls       []Call                // every tool call, refused ones included
}

// Runner runs one feature for one prompt.
type Runner interface {
	// Run drives the model until it ends its turn. A transient model fault (rate limit,
	// overload) ends the run with RetryQueued and no error. triggers are quoted before the
	// first message.
	Run(ctx context.Context, spec Spec, p Principal, prompt string, triggers ...Trigger) (Outcome, error)
}

// Sentinel errors.
var (
	// ErrNoEndTurn: the model kept calling tools past the turn bound.
	ErrNoEndTurn = errors.New("run: no end_turn within the turn bound")
	// ErrTimeout: the model call timed out.
	ErrTimeout = errors.New("run: model timeout")
	// ErrNotAgent: the principal is not an agent principal with a run id.
	ErrNotAgent = errors.New("run: principal must be an agent with a run id")
	// ErrBadSpec: the spec has no feature id.
	ErrBadSpec = errors.New("run: invalid spec")
	// ErrBadOutput: the final output fails the spec's OutputSchema.
	ErrBadOutput = errors.New("run: output fails schema")
)
