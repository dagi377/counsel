package agenttoolsfake

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

func ok(context.Context, registry.Principal, json.RawMessage) (registry.ToolResult, error) {
	return registry.ToolResult{Output: json.RawMessage(`{}`)}, nil
}

// rule: ARC-I-035
func TestARC_I_035_Register(t *testing.T) {
	tests := []struct {
		name string
		tool agenttools.Tool
		err  error
	}{
		{"read", agenttools.Tool{Name: "get_ticket", Kind: registry.ToolRead, Handle: ok}, nil},
		{"write", agenttools.Tool{Name: "draft_reply", Kind: registry.ToolWrite, Handle: ok}, nil},
		{"duplicate", agenttools.Tool{Name: "get_ticket", Kind: registry.ToolRead, Handle: ok}, agenttools.ErrBadTool},
		{"approve prefix", agenttools.Tool{Name: "approve_reply", Kind: registry.ToolWrite, Handle: ok}, agenttools.ErrBadTool},
		{"send prefix", agenttools.Tool{Name: "send_reply", Kind: registry.ToolWrite, Handle: ok}, agenttools.ErrBadTool},
		{"bad kind", agenttools.Tool{Name: "x", Kind: "admin", Handle: ok}, agenttools.ErrBadTool},
		{"no handler", agenttools.Tool{Name: "y", Kind: registry.ToolRead}, agenttools.ErrBadTool},
	}
	f := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := f.Register(tt.tool); !errors.Is(err, tt.err) {
				t.Errorf("Register() = %v, want %v", err, tt.err)
			}
		})
	}
}

// rule: ARC-I-039
func TestARC_I_039_PlanRequired(t *testing.T) {
	ctx := context.Background()
	agent := registry.Principal{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: "u1", Grant: []string{"desk.draft"}}
	noGrant := registry.Principal{Kind: registry.PrincipalAgent, RunID: "r2", ActingFor: "u2"}
	f := New()
	for _, tl := range []agenttools.Tool{
		{Name: "draft_reply", Kind: registry.ToolWrite, Capability: "desk.draft", Handle: ok},
		{Name: "get_ticket", Kind: registry.ToolRead, Handle: ok},
	} {
		if err := f.Register(tl); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name  string
		p     registry.Principal
		tool  string
		in    string
		setup func()
		err   error
	}{
		{"read without plan", agent, "get_ticket", `{}`, nil, nil},
		{"write without plan", agent, "draft_reply", `{}`, nil, agenttools.ErrPlanRequired},
		{"missing capability", noGrant, "draft_reply", `{}`, nil, agenttools.ErrMissingCapability},
		{"unknown", agent, "nope", `{}`, nil, agenttools.ErrUnknownTool},
		{"bad json", agent, "get_ticket", `{`, nil, agenttools.ErrInvalidInput},
		{"write after plan", agent, "draft_reply", `{}`, func() {
			if _, err := f.ReportPlan(ctx, "r1", []string{"draft-reply"}); err != nil {
				t.Fatal(err)
			}
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			if _, err := f.Call(ctx, tt.p, tt.tool, json.RawMessage(tt.in)); !errors.Is(err, tt.err) {
				t.Errorf("Call() = %v, want %v", err, tt.err)
			}
		})
	}
	plan, step, err := f.Current(ctx, "r1")
	if err != nil || step != 0 || len(plan.Steps[0].CallIDs) != 1 {
		t.Errorf("Current() = %+v, %d, %v", plan, step, err)
	}
}

func TestFakePlanResume(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		titles []string
		done   int
		want   int
		err    error
	}{
		{"fresh", []string{"a", "b", "c"}, 0, 0, nil},
		{"resume at third", []string{"a", "b", "c"}, 2, 2, nil},
		{"all done", []string{"a"}, 1, -1, nil},
		{"title too long", []string{strings.Repeat("x", agenttools.MaxStepTitle+1)}, 0, 0, agenttools.ErrStepTitle},
		{"empty plan", nil, 0, 0, agenttools.ErrStepTitle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			p, err := f.ReportPlan(ctx, "r", tt.titles)
			if !errors.Is(err, tt.err) {
				t.Fatalf("ReportPlan() = %v, want %v", err, tt.err)
			}
			if err != nil {
				return
			}
			for i := 0; i < tt.done; i++ {
				if err := f.ReportStep(ctx, p.ID, i, agenttools.StepDone); err != nil {
					t.Fatal(err)
				}
			}
			if _, got, _ := f.Current(ctx, "r"); got != tt.want {
				t.Errorf("first unfinished = %d, want %d", got, tt.want)
			}
		})
	}
	if err := New().ReportStep(ctx, "nope", 0, agenttools.StepDone); !errors.Is(err, agenttools.ErrNotFound) {
		t.Errorf("ReportStep(nope) = %v", err)
	}
}
