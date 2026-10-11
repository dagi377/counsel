package agenttools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

var _ Tools = (*Registry)(nil)

// Registry holds the tools modules register and runs calls against them. It checks
// capability, the tool's JSON Schema and, for an agent's write tools, that the run has
// reported a plan.
type Registry struct {
	plans Plans

	mu    sync.RWMutex
	tools map[string]registered
}

type registered struct {
	tool   Tool
	schema *jsonschema.Schema
}

// NewRegistry returns an empty Registry that looks up run plans in plans.
func NewRegistry(plans Plans) *Registry {
	return &Registry{plans: plans, tools: map[string]registered{}}
}

// Register implements Tools.
func (r *Registry) Register(t Tool) error {
	// rule: ARC-I-035
	switch t.Kind {
	case registry.ToolRead, registry.ToolReport, registry.ToolWrite:
	default:
		return fmt.Errorf("register %q: kind %q: %w", t.Name, t.Kind, ErrBadTool)
	}
	// rule: ARC-I-035
	for _, p := range ForbiddenPrefixes {
		if strings.HasPrefix(t.Name, p) {
			return fmt.Errorf("register %q: forbidden prefix %q: %w", t.Name, p, ErrBadTool)
		}
	}
	if t.Name == "" || t.Handle == nil {
		return fmt.Errorf("register %q: name and handler required: %w", t.Name, ErrBadTool)
	}
	schema, err := compileSchema(t.Name, t.Input)
	if err != nil {
		return fmt.Errorf("register %q: %w: %w", t.Name, ErrBadTool, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.tools[t.Name]; dup {
		return fmt.Errorf("register %q: duplicate: %w", t.Name, ErrBadTool)
	}
	r.tools[t.Name] = registered{tool: t, schema: schema}
	return nil
}

func compileSchema(name string, raw []byte) (*jsonschema.Schema, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("input schema required")
	}
	c := jsonschema.NewCompiler()
	url := "tool://" + name + "/input.json"
	if err := c.AddResource(url, bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("input schema: %w", err)
	}
	s, err := c.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("input schema: %w", err)
	}
	return s, nil
}

// Call implements Tools.
func (r *Registry) Call(ctx context.Context, p Principal, name string, in json.RawMessage) (ToolResult, error) {
	r.mu.RLock()
	reg, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return ToolResult{}, fmt.Errorf("call %q: %w", name, ErrUnknownTool)
	}
	t := reg.tool
	agent := p.Kind == registry.PrincipalAgent
	// rule: ARC-I-036
	if agent && t.Capability != "" && !slices.Contains(p.Grant, t.Capability) {
		return ToolResult{}, fmt.Errorf("call %q: %w", name, ErrMissingCapability)
	}
	var v any
	if err := json.Unmarshal(in, &v); err != nil {
		return ToolResult{}, fmt.Errorf("call %q: %w", name, ErrInvalidInput)
	}
	if err := reg.schema.Validate(v); err != nil {
		// The validation detail can echo input values; keep it out of the error.
		return ToolResult{}, fmt.Errorf("call %q: %w", name, ErrInvalidInput)
	}
	// rule: ARC-I-039
	if agent && t.Kind == registry.ToolWrite {
		if _, _, err := r.plans.Current(ctx, p.RunID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ToolResult{}, fmt.Errorf("call %q: %w", name, ErrPlanRequired)
			}
			return ToolResult{}, fmt.Errorf("call %q: plan: %w", name, err)
		}
	}
	// rule: ARC-U-029
	// The same handler serves the manual path: a user principal calls a tool without an agent
	// run or plan, so every agent action stays usable while the agent is unavailable.
	res, err := t.Handle(ctx, p, in)
	if err != nil {
		return ToolResult{}, fmt.Errorf("call %q: %w", name, err)
	}
	// rule: ARC-I-035
	// Every write-kind tool is held to WriteTool's contract here, so a write tool registered
	// without WriteTool still cannot send, approve or execute: its result must be a draft, a
	// proposal or an opened gate.
	if t.Kind == registry.ToolWrite {
		if err := CheckWriteResult(res); err != nil {
			return ToolResult{}, fmt.Errorf("call %q: %w", name, err)
		}
	}
	return res, nil
}
