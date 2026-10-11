package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	reportMatter = "00000000-0000-4000-8000-000000000501"
	reportWalled = "00000000-0000-4000-8000-000000000502"
	reportCap    = "agent.ask"
)

// planStore is an in-memory agenttools.PlanStore, so the tests drive the real Planner.
type planStore struct {
	mu    sync.Mutex
	plans map[string]agenttools.Plan
}

func (s *planStore) ByRun(_ context.Context, runID string) (agenttools.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.plans {
		if p.RunID == runID {
			return clonePlan(p), nil
		}
	}
	return agenttools.Plan{}, agenttools.ErrNotFound
}

func (s *planStore) ByID(_ context.Context, id string) (agenttools.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[id]
	if !ok {
		return agenttools.Plan{}, agenttools.ErrNotFound
	}
	return clonePlan(p), nil
}

func (s *planStore) Save(_ context.Context, p agenttools.Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, old := range s.plans {
		if old.RunID == p.RunID && id != p.ID {
			delete(s.plans, id)
		}
	}
	s.plans[p.ID] = clonePlan(p)
	return nil
}

func clonePlan(p agenttools.Plan) agenttools.Plan {
	p.Steps = append([]agenttools.Step(nil), p.Steps...)
	return p
}

type reportFixture struct {
	reg   *agenttools.Registry
	plans *agenttools.Planner
}

func newReportRegistry(t *testing.T, actorCaps map[string]bool) reportFixture {
	t.Helper()
	walls := rolesfake.New()
	walls.Wall(clerk, reportWalled)
	planner := agenttools.NewPlanner(&planStore{plans: map[string]agenttools.Plan{}})
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	reg := agenttools.NewRegistry(planner)
	for _, h := range agent.ReportHandlers() {
		tool, err := h.Build(agent.NewDeps(can, walls, planner))
		if err != nil {
			t.Fatalf("build %s: %v", h.Name, err)
		}
		if err := reg.Register(tool); err != nil {
			t.Fatalf("register %s: %v", h.Name, err)
		}
	}
	return reportFixture{reg: reg, plans: planner}
}

func planInput(titles ...string) string {
	var steps []string
	for i, title := range titles {
		steps = append(steps, fmt.Sprintf(`{"step_id":"step-%d","title":%q}`, i+1, title))
	}
	return `{"steps":[` + strings.Join(steps, ",") + `]}`
}

// rule: ARC-I-039
// rule: ARC-I-036
func TestARC_I_039_ReportPlanAndStep(t *testing.T) {
	granted := []string{reportCap}
	asks := map[string]bool{reportCap: true}
	twoSteps := planInput("Read the matter", "draft-bill")
	cases := []struct {
		name      string
		grant     []string
		actorCaps map[string]bool
		plan      string // report_plan input; empty skips report_plan
		tool      string
		in        string
		wantErr   error
		wantSteps []agenttools.StepStatus // plan step statuses after the call
	}{
		{"plan stores pending steps", granted, asks, "", agent.ToolReportPlan, twoSteps, nil,
			[]agenttools.StepStatus{agenttools.StepPending, agenttools.StepPending}},
		// The Registry records every agent call against the first unfinished step, so the
		// report_step call itself marks step-1 running.
		{"step by id", granted, asks, twoSteps, agent.ToolReportStep, `{"step_id":"step-2","status":"done"}`, nil,
			[]agenttools.StepStatus{agenttools.StepRunning, agenttools.StepDone}},
		{"skipped is finished", granted, asks, twoSteps, agent.ToolReportStep, `{"step_id":"step-1","status":"skipped"}`, nil,
			[]agenttools.StepStatus{agenttools.StepDone, agenttools.StepPending}},
		{"waiting on a gate is running", granted, asks, twoSteps, agent.ToolReportStep,
			`{"step_id":"step-1","status":"waiting_gate","outputs":[{"type":"matter","id":"` + reportMatter + `"}]}`, nil,
			[]agenttools.StepStatus{agenttools.StepRunning, agenttools.StepPending}},
		{"title over 140 chars is 422", granted, asks, "", agent.ToolReportPlan, planInput(strings.Repeat("t", 141)), agenttools.ErrInvalidInput, nil},
		{"title of 140 chars passes", granted, asks, "", agent.ToolReportPlan, planInput(strings.Repeat("t", 140)), nil,
			[]agenttools.StepStatus{agenttools.StepPending}},
		{"blank title is 422", granted, asks, "", agent.ToolReportPlan, planInput("  "), agenttools.ErrStepTitle, nil},
		{"repeated step id", granted, asks, "", agent.ToolReportPlan, `{"steps":[{"step_id":"a","title":"x"},{"step_id":"a","title":"y"}]}`, agent.ErrInvalidInput, nil},
		{"schema: plan without steps", granted, asks, "", agent.ToolReportPlan, `{"steps":[]}`, agenttools.ErrInvalidInput, nil},
		{"schema: unknown status", granted, asks, twoSteps, agent.ToolReportStep, `{"step_id":"step-1","status":"approved"}`, agenttools.ErrInvalidInput, nil},
		{"schema: extra field", granted, asks, twoSteps, agent.ToolReportStep, `{"step_id":"step-1","status":"done","note":"x"}`, agenttools.ErrInvalidInput, nil},
		{"unknown step id", granted, asks, twoSteps, agent.ToolReportStep, `{"step_id":"step-9","status":"done"}`, agenttools.ErrNotFound, nil},
		{"step before any plan", granted, asks, "", agent.ToolReportStep, `{"step_id":"step-1","status":"done"}`, agenttools.ErrNotFound, nil},
		{"walled matter output is not found", granted, asks, twoSteps, agent.ToolReportStep,
			`{"step_id":"step-1","status":"done","outputs":[{"type":"matter","id":"` + reportWalled + `"}]}`, agent.ErrNotVisible,
			[]agenttools.StepStatus{agenttools.StepRunning, agenttools.StepPending}}, // not marked done
		{"agent grant lacks capability", nil, asks, "", agent.ToolReportPlan, twoSteps, agenttools.ErrMissingCapability, nil},
		{"acting user lacks capability", granted, map[string]bool{}, "", agent.ToolReportPlan, twoSteps, agent.ErrForbidden, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newReportRegistry(t, tc.actorCaps)
			p := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: clerk, Grant: tc.grant}
			if tc.plan != "" {
				if _, err := f.reg.Call(ctx, p, agent.ToolReportPlan, json.RawMessage(tc.plan)); err != nil {
					t.Fatalf("report_plan: %v", err)
				}
			}
			res, err := f.reg.Call(ctx, p, tc.tool, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil {
				var out struct {
					Status string `json:"status"`
				}
				if err := json.Unmarshal(res.Output, &out); err != nil || out.Status != "ok" {
					t.Fatalf("output %s: %v", res.Output, err)
				}
			}
			if tc.wantSteps == nil {
				return
			}
			plan, _, err := f.plans.Current(ctx, "run-1")
			if err != nil {
				t.Fatal(err)
			}
			var got []agenttools.StepStatus
			for _, s := range plan.Steps {
				got = append(got, s.Status)
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.wantSteps) {
				t.Fatalf("steps = %v, want %v", got, tc.wantSteps)
			}
		})
	}
}

// rule: ARC-I-039
func TestARC_I_039_ReportPlanUnlocksWrite(t *testing.T) {
	ctx := context.Background()
	f := newReportRegistry(t, map[string]bool{reportCap: true})
	write := registry.Tool{Name: "draft_note", Kind: registry.ToolWrite, Input: []byte(`{"type":"object"}`),
		Handle: func(context.Context, registry.Principal, json.RawMessage) (registry.ToolResult, error) {
			return registry.ToolResult{Output: json.RawMessage(`{"outcome":"gate","ref":"g-1"}`), GateID: "g-1"}, nil
		}}
	if err := f.reg.Register(write); err != nil {
		t.Fatal(err)
	}
	p := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: clerk, Grant: []string{reportCap}}
	steps := []struct {
		tool    string
		in      string
		wantErr error
	}{
		{"draft_note", `{}`, agenttools.ErrPlanRequired},
		{agent.ToolReportPlan, planInput("draft-note"), nil},
		{"draft_note", `{}`, nil},
	}
	for i, s := range steps {
		if _, err := f.reg.Call(ctx, p, s.tool, json.RawMessage(s.in)); !errors.Is(err, s.wantErr) || (s.wantErr == nil && err != nil) {
			t.Fatalf("call %d %s: err = %v, want %v", i, s.tool, err, s.wantErr)
		}
	}
}

func TestReportHandlersNeedDeps(t *testing.T) {
	can := agent.CapabilityCheck(func(context.Context, string, string) (bool, error) { return true, nil })
	cases := []struct {
		name string
		deps agent.Deps
	}{
		{"no deps", agent.NewDeps()},
		{"no walls", agent.NewDeps(can)},
		{"no planner", agent.NewDeps(can, rolesfake.New())},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, h := range agent.ReportHandlers() {
				if _, err := h.Build(tc.deps); !errors.Is(err, agent.ErrMissingDep) {
					t.Fatalf("%s: err = %v, want ErrMissingDep", h.Name, err)
				}
			}
		})
	}
}
