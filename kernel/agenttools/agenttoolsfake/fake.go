// Package agenttoolsfake is an in-memory agenttools.Tools and agenttools.Plans for tests.
// Capabilities are a fixed set per principal; input schemas are not validated beyond JSON
// well-formedness.
package agenttoolsfake

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

var (
	_ agenttools.Tools = (*Fake)(nil)
	_ agenttools.Plans = (*Fake)(nil)
)

// Fake keeps tools, plans and call records in memory.
type Fake struct {
	mu    sync.Mutex
	tools map[string]agenttools.Tool
	plans map[string]*agenttools.Plan // by run id
	calls int
}

// New returns an empty Fake.
func New() *Fake {
	return &Fake{tools: map[string]agenttools.Tool{}, plans: map[string]*agenttools.Plan{}}
}

// Register implements agenttools.Tools.
func (f *Fake) Register(t agenttools.Tool) error {
	// rule: ARC-I-035
	switch t.Kind {
	case registry.ToolRead, registry.ToolReport, registry.ToolWrite:
	default:
		return fmt.Errorf("register %q: kind %q: %w", t.Name, t.Kind, agenttools.ErrBadTool)
	}
	for _, p := range agenttools.ForbiddenPrefixes {
		if strings.HasPrefix(t.Name, p) {
			return fmt.Errorf("register %q: %w", t.Name, agenttools.ErrBadTool)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, dup := f.tools[t.Name]; dup || t.Name == "" || t.Handle == nil {
		return fmt.Errorf("register %q: %w", t.Name, agenttools.ErrBadTool)
	}
	f.tools[t.Name] = t
	return nil
}

// Call implements agenttools.Tools.
func (f *Fake) Call(ctx context.Context, p agenttools.Principal, name string, in json.RawMessage) (agenttools.ToolResult, error) {
	f.mu.Lock()
	t, ok := f.tools[name]
	plan := f.plans[p.RunID]
	f.mu.Unlock()
	if !ok {
		return agenttools.ToolResult{}, fmt.Errorf("call %q: %w", name, agenttools.ErrUnknownTool)
	}
	// rule: ARC-I-036
	if t.Capability != "" && p.Kind == registry.PrincipalAgent && !slices.Contains(p.Grant, t.Capability) {
		return agenttools.ToolResult{}, fmt.Errorf("call %q: %w", name, agenttools.ErrMissingCapability)
	}
	if !json.Valid(in) {
		return agenttools.ToolResult{}, fmt.Errorf("call %q: %w", name, agenttools.ErrInvalidInput)
	}
	// rule: ARC-I-039
	if t.Kind == registry.ToolWrite && p.Kind == registry.PrincipalAgent && plan == nil {
		return agenttools.ToolResult{}, fmt.Errorf("call %q: %w", name, agenttools.ErrPlanRequired)
	}
	res, err := t.Handle(ctx, p, in)
	if err != nil {
		return agenttools.ToolResult{}, fmt.Errorf("call %q: %w", name, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if plan != nil {
		if i := firstOpen(plan); i >= 0 {
			plan.Steps[i].CallIDs = append(plan.Steps[i].CallIDs, fmt.Sprintf("call-%d", f.calls))
		}
	}
	return res, nil
}

func firstOpen(p *agenttools.Plan) int {
	for i, s := range p.Steps {
		if s.Status != agenttools.StepDone {
			return i
		}
	}
	return -1
}

// ReportPlan implements agenttools.Plans.
func (f *Fake) ReportPlan(_ context.Context, runID string, titles []string) (agenttools.Plan, error) {
	if len(titles) == 0 {
		return agenttools.Plan{}, fmt.Errorf("report plan: no steps: %w", agenttools.ErrStepTitle)
	}
	p := agenttools.Plan{ID: "plan-" + runID, RunID: runID}
	for i, title := range titles {
		// rule: ARC-I-039
		if strings.TrimSpace(title) == "" || len([]rune(title)) > agenttools.MaxStepTitle {
			return agenttools.Plan{}, fmt.Errorf("report plan: step %d: %w", i, agenttools.ErrStepTitle)
		}
		p.Steps = append(p.Steps, agenttools.Step{Index: i, Title: title, Status: agenttools.StepPending})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plans[runID] = &p
	return clonePlan(&p), nil
}

// ReportStep implements agenttools.Plans.
func (f *Fake) ReportStep(_ context.Context, planID string, index int, status agenttools.StepStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.plans {
		if p.ID == planID && index >= 0 && index < len(p.Steps) {
			p.Steps[index].Status = status
			return nil
		}
	}
	return fmt.Errorf("report step %s/%d: %w", planID, index, agenttools.ErrNotFound)
}

// Current implements agenttools.Plans.
func (f *Fake) Current(_ context.Context, runID string) (agenttools.Plan, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.plans[runID]
	if !ok {
		return agenttools.Plan{}, -1, fmt.Errorf("plan for run: %w", agenttools.ErrNotFound)
	}
	return clonePlan(p), firstOpen(p), nil
}

func clonePlan(p *agenttools.Plan) agenttools.Plan {
	out := *p
	out.Steps = make([]agenttools.Step, len(p.Steps))
	for i, s := range p.Steps {
		s.CallIDs = slices.Clone(s.CallIDs)
		out.Steps[i] = s
	}
	return out
}
