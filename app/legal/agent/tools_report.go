package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// Report tool names (the kernel's plan bookkeeping tools).
const (
	ToolReportPlan = agenttools.ToolReportPlan
	ToolReportStep = agenttools.ToolReportStep
)

// ReportOutputMatter is the report_step output type that names a matter.
const ReportOutputMatter = "matter"

// ReportHandlers returns the report_plan and report_step handlers. They do not register
// themselves: boot adds them to the agent HandlerSet with the other tool handlers. Both
// share one step-id index, so build them from the same call.
func ReportHandlers() []Handler {
	r := &reportTools{steps: map[string][]string{}}
	return []Handler{
		{Name: ToolReportPlan, Build: r.buildPlan},
		{Name: ToolReportStep, Build: r.buildStep},
	}
}

// reportTools keeps, per run, the step ids of the run's plan in plan order, so report_step
// can name a step by id. A run that resumes reports its plan again, which the Planner
// answers with the stored plan, and the index is rebuilt.
type reportTools struct {
	mu    sync.Mutex
	steps map[string][]string // run id -> step ids
}

// reportDeps reads Deps: CapabilityCheck, roles.Walls, agenttools.Plans.
func reportDeps(name string, d Deps) (CapabilityCheck, roles.Walls, agenttools.Plans, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	plans, err := DepOf[agenttools.Plans](d)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	return can, walls, plans, nil
}

type reportPlanIn struct {
	Steps []struct {
		StepID string `json:"step_id"`
		Title  string `json:"title"`
	} `json:"steps"`
}

type reportPlanOut struct {
	Status string `json:"status"`
	PlanID string `json:"plan_id"`
}

func (r *reportTools) buildPlan(d Deps) (registry.Tool, error) {
	can, _, plans, err := reportDeps(ToolReportPlan, d)
	if err != nil {
		return registry.Tool{}, err
	}
	return reportTool(ToolReportPlan, can, func(ctx context.Context, _ string, runID string, raw json.RawMessage) (registry.ToolResult, error) {
		var in reportPlanIn
		if err := json.Unmarshal(raw, &in); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolReportPlan, ErrInvalidInput)
		}
		ids := make([]string, 0, len(in.Steps))
		titles := make([]string, 0, len(in.Steps))
		for _, s := range in.Steps {
			if slices.Contains(ids, s.StepID) {
				return registry.ToolResult{}, fmt.Errorf("%s: step id repeated: %w", ToolReportPlan, ErrInvalidInput)
			}
			ids = append(ids, s.StepID)
			titles = append(titles, s.Title)
		}
		// rule: ARC-I-039
		// The Planner refuses an empty or over-long title (422) whatever the schema said.
		plan, err := plans.ReportPlan(ctx, runID, titles)
		if err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolReportPlan, err)
		}
		// A Plans that stores step ids (the Planner over PGPlans) keeps them across a restart;
		// the in-process map covers a Plans that does not.
		if named, ok := plans.(agenttools.StepIDs); ok {
			if err := named.SetStepIDs(ctx, plan.ID, ids); err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolReportPlan, err)
			}
		}
		r.mu.Lock()
		r.steps[runID] = ids
		r.mu.Unlock()
		return jsonResult(ToolReportPlan, reportPlanOut{Status: "ok", PlanID: plan.ID})
	})
}

type reportStepIn struct {
	StepID  string `json:"step_id"`
	Status  string `json:"status"`
	Outputs []struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"outputs"`
}

type reportStepOut struct {
	Status string `json:"status"`
	StepID string `json:"step_id"`
	Index  int    `json:"index"`
}

// stepStatus maps the a2 report_step status onto the kernel plan step status. A skipped
// step is finished; a step waiting on a gate is still running.
var stepStatus = map[string]agenttools.StepStatus{
	"running":      agenttools.StepRunning,
	"done":         agenttools.StepDone,
	"failed":       agenttools.StepFailed,
	"skipped":      agenttools.StepDone,
	"waiting_gate": agenttools.StepRunning,
}

func (r *reportTools) buildStep(d Deps) (registry.Tool, error) {
	can, walls, plans, err := reportDeps(ToolReportStep, d)
	if err != nil {
		return registry.Tool{}, err
	}
	return reportTool(ToolReportStep, can, func(ctx context.Context, user, runID string, raw json.RawMessage) (registry.ToolResult, error) {
		var in reportStepIn
		if err := json.Unmarshal(raw, &in); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolReportStep, ErrInvalidInput)
		}
		status, ok := stepStatus[in.Status]
		if !ok {
			return registry.ToolResult{}, fmt.Errorf("%s: status: %w", ToolReportStep, ErrInvalidInput)
		}
		for _, o := range in.Outputs {
			if o.Type != ReportOutputMatter {
				continue
			}
			// A walled matter is not found, not forbidden.
			if err := visibleTo(ctx, walls, user, o.ID); err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolReportStep, err)
			}
		}
		plan, _, err := plans.Current(ctx, runID)
		if err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolReportStep, err)
		}
		index := agenttools.StepIndex(plan, in.StepID)
		if index < 0 {
			r.mu.Lock()
			index = slices.Index(r.steps[runID], in.StepID)
			r.mu.Unlock()
		}
		if index < 0 || index >= len(plan.Steps) {
			return registry.ToolResult{}, fmt.Errorf("%s: step: %w", ToolReportStep, agenttools.ErrNotFound)
		}
		if err := plans.ReportStep(ctx, plan.ID, index, status); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolReportStep, err)
		}
		return jsonResult(ToolReportStep, reportStepOut{Status: "ok", StepID: in.StepID, Index: index})
	})
}

// reportTool builds a report tool the way handlerTool builds a read tool (a2 name, kind,
// capability and input schema; the acting user must hold the capability) and also hands the
// handler the caller's run id: a report tool acts on the caller's run plan.
func reportTool(name string, can CapabilityCheck, handle func(ctx context.Context, user, runID string, in json.RawMessage) (registry.ToolResult, error)) (registry.Tool, error) {
	if can == nil {
		return registry.Tool{}, fmt.Errorf("tool %s: capability check: %w", name, ErrMissingDep)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: name, Kind: Report}})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("tool %s: %w", name, err)
	}
	s := specs[0]
	return registry.Tool{
		Name: s.Name, Kind: registry.ToolKind(s.Kind), Capability: s.Capability, Input: s.InputSchema,
		Handle: func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
			user := actor(p)
			// rule: ARC-I-036
			ok, err := can(ctx, user, s.Capability)
			if err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: capability: %w", name, err)
			}
			if !ok {
				return registry.ToolResult{}, fmt.Errorf("%s: %w", name, ErrForbidden)
			}
			if p.RunID == "" {
				return registry.ToolResult{}, fmt.Errorf("%s: run id required: %w", name, agenttools.ErrNotFound)
			}
			return handle(ctx, user, p.RunID, in)
		},
	}, nil
}

func jsonResult(name string, v any) (registry.ToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", name, err)
	}
	return registry.ToolResult{Output: b}, nil
}
