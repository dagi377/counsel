package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/work"
	"github.com/dagi377/counsel/kernel/work/workfake"
)

const shiftRun = "run-shift"

// shiftWorld registers the real shift_date and create_task on one Registry over one
// Provenance. shiftRun has a plan and has read readDate.
type shiftWorld struct {
	reg   *agenttools.Registry
	items *workfake.Fake
	pv    *agenttools.Provenance
}

func newShiftWorld(t *testing.T, readDates ...string) shiftWorld {
	t.Helper()
	ctx := context.Background()
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && (capability == taskCap || capability == agent.CapDeadlinesView), nil
	})
	w := shiftWorld{items: workfake.New(), pv: agenttools.NewProvenance()}
	for _, d := range readDates {
		w.pv.Observe(shiftRun, json.RawMessage(`{"due_date":"`+d+`"}`))
	}
	deps := agent.NewDeps(can, rolesfake.New(), w.items, proposedTasks{}, w.pv)
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, shiftRun, []string{"propose a task"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	w.reg = agenttools.NewRegistry(plans)
	for _, h := range []agent.Handler{agent.ShiftDateHandler(), agent.CreateTaskHandler()} {
		tool, err := h.Build(deps)
		if err != nil {
			t.Fatalf("build %s: %v", h.Name, err)
		}
		if err := w.reg.Register(tool); err != nil {
			t.Fatalf("register %s: %v", h.Name, err)
		}
	}
	return w
}

func shiftAgent() registry.Principal {
	return registry.Principal{Kind: registry.PrincipalAgent, RunID: shiftRun, ActingFor: clerk,
		Grant: []string{taskCap, agent.CapDeadlinesView}}
}

// TestShiftDate shifts by calendar and business days. Business days skip weekends and the
// Ontario court holidays (Thanksgiving 2026-10-12; Christmas 2026-12-25 and the Monday after,
// 2026-12-28).
//
// rule: ARC-U-027
func TestShiftDate(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{"calendar days ignore weekends", `{"date":"2026-10-09","days":3,"unit":"calendar"}`, "2026-10-12", nil},
		{"calendar days back", `{"date":"2026-10-09","days":-7,"unit":"calendar"}`, "2026-10-02", nil},
		{"business day over a weekend", `{"date":"2026-10-16","days":1,"unit":"business"}`, "2026-10-19", nil},
		{"business day over a weekend and Thanksgiving", `{"date":"2026-10-09","days":1,"unit":"business"}`, "2026-10-13", nil},
		{"business day back over Thanksgiving", `{"date":"2026-10-13","days":-1,"unit":"business"}`, "2026-10-09", nil},
		{"business days over Christmas", `{"date":"2026-12-24","days":1,"unit":"business"}`, "2026-12-29", nil},
		{"zero business days", `{"date":"2026-10-10","days":0,"unit":"business"}`, "2026-10-10", nil},
		{"unknown unit", `{"date":"2026-10-09","days":1,"unit":"weeks"}`, "", agenttools.ErrInvalidInput},
		{"start date the run did not read", `{"date":"2026-10-05","days":1,"unit":"business"}`, "", agenttools.ErrUnsourcedValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newShiftWorld(t, "2026-10-09", "2026-10-16", "2026-10-13", "2026-12-24", "2026-10-10")
			res, err := w.reg.Call(context.Background(), shiftAgent(), agent.ToolShiftDate, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			var out struct {
				Date string `json:"date"`
			}
			if err := json.Unmarshal(res.Output, &out); err != nil || out.Date != tc.want {
				t.Fatalf("output = %s, want date %s", res.Output, tc.want)
			}
		})
	}
}

// TestARC_U_027_CreateTaskDueOnSourced: create_task's due_on must be a date the run read. A
// shift_date result counts as read; an invented date is refused before any item is created.
//
// rule: ARC-U-027
func TestARC_U_027_CreateTaskDueOnSourced(t *testing.T) {
	cases := []struct {
		name    string
		shift   string // shift_date input run first, when set
		dueOn   string
		p       registry.Principal
		wantErr error
	}{
		{"shifted date", `{"date":"2026-10-09","days":1,"unit":"business"}`, "2026-10-13", shiftAgent(), nil},
		{"date the run read", "", "2026-10-09", shiftAgent(), nil},
		{"invented date", "", "2026-10-14", shiftAgent(), agenttools.ErrUnsourcedValue},
		{"date next to a shifted one", `{"date":"2026-10-09","days":1,"unit":"business"}`, "2026-10-14", shiftAgent(), agenttools.ErrUnsourcedValue},
		{"manual path is not checked", "", "2026-10-14", registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newShiftWorld(t, "2026-10-09")
			if tc.shift != "" {
				if _, err := w.reg.Call(ctx, tc.p, agent.ToolShiftDate, json.RawMessage(tc.shift)); err != nil {
					t.Fatalf("shift_date: %v", err)
				}
			}
			in := taskInput(map[string]string{"title": "File the reply", "due_on": tc.dueOn})
			_, err := w.reg.Call(ctx, tc.p, agent.ToolCreateTask, json.RawMessage(in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			items, _ := w.items.List(ctx, work.Filter{})
			if want := tc.wantErr == nil; (len(items) == 1) != want {
				t.Fatalf("items = %d, want created %v", len(items), want)
			}
		})
	}
}
