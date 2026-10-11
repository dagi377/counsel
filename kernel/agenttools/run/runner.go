package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/registry"
)

// DefaultMaxTurns bounds the model calls of one run.
const DefaultMaxTurns = 10

// triggerTool names the tool_result blocks that carry quoted trigger text.
const triggerTool = "trigger"

var _ Runner = (*Loop)(nil)

// Loop is the Runner: a model client and the agent tools it calls.
type Loop struct {
	model    ai.ModelClient
	tools    agenttools.Tools
	maxTurns int
}

// Option configures a Loop.
type Option func(*Loop)

// WithMaxTurns sets the model call bound (default DefaultMaxTurns).
func WithMaxTurns(n int) Option {
	return func(l *Loop) {
		if n > 0 {
			l.maxTurns = n
		}
	}
}

// New returns a Loop over model and tools.
func New(model ai.ModelClient, tools agenttools.Tools, opts ...Option) *Loop {
	l := &Loop{model: model, tools: tools, maxTurns: DefaultMaxTurns}
	for _, o := range opts {
		o(l)
	}
	return l
}

// Run implements Runner.
func (l *Loop) Run(ctx context.Context, spec Spec, p Principal, prompt string, triggers ...Trigger) (Outcome, error) {
	if spec.FeatureID == "" {
		return Outcome{}, fmt.Errorf("run: feature id: %w", ErrBadSpec)
	}
	if p.Kind != registry.PrincipalAgent || p.RunID == "" {
		return Outcome{}, fmt.Errorf("run %s: %w", spec.FeatureID, ErrNotAgent)
	}
	var schema *jsonschema.Schema
	if len(bytes.TrimSpace(spec.OutputSchema)) > 0 {
		var err error
		if schema, err = compile(spec.FeatureID, spec.OutputSchema); err != nil {
			return Outcome{}, fmt.Errorf("run %s: %w: %w", spec.FeatureID, ErrBadSpec, err)
		}
	}
	// rule: ARC-I-036
	// Calls run as the agent principal with only the spec's grant.
	agent := Principal{Kind: registry.PrincipalAgent, RunID: p.RunID, ActingFor: p.ActingFor, Grant: slices.Clone(spec.Grant)}
	kind, alias := spec.Kind, spec.Model
	if kind == "" {
		kind = ai.KindToolUse
	}
	if alias == "" {
		alias = "drafting"
	}
	msgs, err := firstMessages(prompt, triggers)
	if err != nil {
		return Outcome{}, fmt.Errorf("run %s: %w", spec.FeatureID, err)
	}
	var out Outcome
	for range l.maxTurns {
		req := ai.Request{
			FeatureID: spec.FeatureID, Kind: kind, Model: alias, System: spec.System,
			Tools: spec.Tools, Messages: slices.Clone(msgs), Schema: spec.OutputSchema,
			Metadata: map[string]string{"run_id": p.RunID, "acting_for": p.ActingFor},
		}
		resp, err := l.model.Complete(ctx, req)
		if err != nil {
			// rule: AI-R-003
			// A transient fault queues a retry; no tool runs after it.
			if errors.Is(err, ai.ErrRateLimited) || errors.Is(err, ai.ErrOverloaded) {
				slog.InfoContext(ctx, "agent run retry queued", "feature", spec.FeatureID, "run", p.RunID, "calls", len(out.Calls))
				out.RetryQueued = true
				return out, nil
			}
			if errors.Is(err, ai.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
				return out, fmt.Errorf("run %s: %w: %w", spec.FeatureID, ErrTimeout, err)
			}
			return out, fmt.Errorf("run %s: model: %w", spec.FeatureID, err)
		}
		if len(resp.ToolCalls) == 0 {
			out.Text, out.JSON = resp.Text, resp.JSON
			if schema != nil {
				if err := validate(schema, resp.JSON); err != nil {
					return out, fmt.Errorf("run %s: %w", spec.FeatureID, err)
				}
			}
			slog.InfoContext(ctx, "agent run done", "feature", spec.FeatureID, "run", p.RunID, "calls", len(out.Calls), "gates", len(out.GateIDs))
			return out, nil
		}
		uses := ai.Message{Role: "assistant"}
		results := ai.Message{Role: "tool"}
		for _, tc := range resp.ToolCalls {
			uses.Content = append(uses.Content, ai.ContentBlock{Type: "tool_use", ToolUseID: tc.ID, ToolName: tc.Name, Input: tc.Input})
			// rule: ARC-I-039
			// Every call goes through the Registry, which refuses a write before report_plan.
			res, err := l.tools.Call(ctx, agent, tc.Name, tc.Input)
			// rule: ARC-I-038
			// A tool's output reaches the model only as a tool_result block.
			block := ai.ContentBlock{Type: "tool_result", ToolUseID: tc.ID, ToolName: tc.Name}
			call := Call{Tool: tc.Name}
			if err != nil {
				// rule: ARC-I-035
				// A refused call goes back as an error result carrying only its problem code,
				// never handler detail.
				call.Refused, call.Err = Code(err), err
				block.IsError, block.Text = true, call.Refused
			} else {
				block.Text = string(res.Output)
				out.Citations = appendNew(out.Citations, res.Citations...)
				if res.GateID != "" {
					out.GateIDs = append(out.GateIDs, res.GateID)
				}
			}
			out.Calls = append(out.Calls, call)
			results.Content = append(results.Content, block)
		}
		msgs = append(msgs, uses, results)
	}
	return out, fmt.Errorf("run %s: %d turns: %w", spec.FeatureID, l.maxTurns, ErrNoEndTurn)
}

// firstMessages builds the opening user message: the prompt as text, then each trigger as a
// quoted tool_result block, so untrusted text never sits among instructions.
func firstMessages(prompt string, triggers []Trigger) ([]ai.Message, error) {
	first := ai.Message{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: prompt}}}
	for i, t := range triggers {
		// rule: ARC-I-038
		q, err := json.Marshal(agenttools.Quote(t.Source, t.Text))
		if err != nil {
			return nil, fmt.Errorf("quote trigger %d: %w", i, err)
		}
		first.Content = append(first.Content, ai.ContentBlock{
			Type: "tool_result", ToolUseID: fmt.Sprintf("%s-%d", triggerTool, i), ToolName: triggerTool, Text: string(q),
		})
	}
	return []ai.Message{first}, nil
}

// Code maps a tool refusal to its problem+json code.
func Code(err error) string {
	switch {
	case errors.Is(err, agenttools.ErrPlanRequired):
		return "plan_required"
	case errors.Is(err, agenttools.ErrMissingCapability):
		return "missing_capability"
	case errors.Is(err, agenttools.ErrAgentDecides):
		return "agent_cannot_decide"
	case errors.Is(err, agenttools.ErrUnknownTool):
		return "unknown_tool"
	case errors.Is(err, agenttools.ErrInvalidInput):
		return "invalid_input"
	case errors.Is(err, agenttools.ErrCitation):
		return "invalid_citation"
	case errors.Is(err, agenttools.ErrNotFound):
		return "not_found"
	}
	return "tool_failed"
}

func appendNew(dst []agenttools.Citation, src ...agenttools.Citation) []agenttools.Citation {
	for _, c := range src {
		if !slices.Contains(dst, c) {
			dst = append(dst, c)
		}
	}
	return dst
}

func compile(feature string, raw json.RawMessage) (*jsonschema.Schema, error) {
	c := jsonschema.NewCompiler()
	url := "run://" + feature + "/output.json"
	if err := c.AddResource(url, bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}
	s, err := c.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}
	return s, nil
}

// validate checks out against schema. The validation detail can echo output values, so it
// stays out of the error.
func validate(schema *jsonschema.Schema, out json.RawMessage) error {
	var v any
	if err := json.Unmarshal(out, &v); err != nil {
		return ErrBadOutput
	}
	if err := schema.Validate(v); err != nil {
		return ErrBadOutput
	}
	return nil
}
