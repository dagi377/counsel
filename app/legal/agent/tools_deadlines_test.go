package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	dlOpenMatter   = "11111111-1111-4111-8111-111111111111"
	dlWalledMatter = "22222222-2222-4222-8222-222222222222"
)

// deadlinesWorld stores synthetic deadlines on a visible matter, a walled matter and a
// non-matter subject, and returns a Registry with the real list_deadlines wrapped so its
// results record into pv.
func deadlinesWorld(t *testing.T, actorCaps map[string]bool) (*agenttools.Registry, *agenttools.Provenance) {
	t.Helper()
	ctx := context.Background()
	engine := deadlinefake.New()
	day := func(d int) clock.Date { return clock.Date{Year: 2026, Month: 3, Day: d} }
	for _, p := range []deadline.Proposed{
		{PackID: "on-civil", PackVersion: "2026.1", RuleID: "defence", Trigger: day(2), Due: day(22), Subject: deadline.Subject{Kind: roles.SubjectMatter, ID: dlOpenMatter}},
		{PackID: "on-civil", PackVersion: "2026.1", RuleID: "reply", Trigger: day(2), Due: day(12), Subject: deadline.Subject{Kind: roles.SubjectMatter, ID: dlOpenMatter}},
		{PackID: "on-civil", PackVersion: "2026.1", RuleID: "defence", Trigger: day(1), Due: day(21), Subject: deadline.Subject{Kind: roles.SubjectMatter, ID: dlWalledMatter}},
		{PackID: "on-civil", PackVersion: "2026.1", RuleID: "defence", Trigger: day(1), Due: day(15), Subject: deadline.Subject{Kind: "other.thing", ID: "x"}},
	} {
		if _, err := engine.Save(ctx, nil, p); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	walls := rolesfake.New()
	walls.Wall(clerk, dlWalledMatter)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	tool, err := agent.ListDeadlinesHandler().Build(agent.NewDeps(can, walls, engine, agenttools.NewProvenance()))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	pv := agenttools.NewProvenance()
	if tool, err = pv.ReadTool(tool); err != nil {
		t.Fatalf("provenance: %v", err)
	}
	reg := agenttools.NewRegistry(agenttoolsfake.New())
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return reg, pv
}

// rule: ARC-I-036
// rule: ARC-I-038
func TestARC_I_036_ListDeadlines(t *testing.T) {
	granted := []string{agent.CapDeadlinesView}
	view := map[string]bool{agent.CapDeadlinesView: true}
	cases := []struct {
		name      string
		grant     []string
		actorCaps map[string]bool
		in        string
		wantErr   error
		wantDue   []string
		wantNext  string
	}{
		{"visible matters only, by due date", granted, view, `{}`, nil, []string{"2026-03-12", "2026-03-22"}, ""},
		{"one matter", granted, view, `{"matter_id":"` + dlOpenMatter + `"}`, nil, []string{"2026-03-12", "2026-03-22"}, ""},
		{"date window", granted, view, `{"from":"2026-03-13","to":"2026-03-31"}`, nil, []string{"2026-03-22"}, ""},
		{"first page", granted, view, `{"limit":1}`, nil, []string{"2026-03-12"}, "1"},
		{"second page", granted, view, `{"limit":1,"cursor":"1"}`, nil, []string{"2026-03-22"}, ""},
		{"walled matter", granted, view, `{"matter_id":"` + dlWalledMatter + `"}`, agent.ErrNotVisible, nil, ""},
		{"run without the capability", nil, view, `{}`, agenttools.ErrMissingCapability, nil, ""},
		{"acting user without the capability", granted, map[string]bool{}, `{}`, agent.ErrForbidden, nil, ""},
		{"unknown field", granted, view, `{"rule_id":"x"}`, agenttools.ErrInvalidInput, nil, ""},
		{"limit too high", granted, view, `{"limit":51}`, agenttools.ErrInvalidInput, nil, ""},
		{"bad date", granted, view, `{"from":"13/03/2026"}`, agent.ErrInvalidInput, nil, ""},
		{"bad cursor", granted, view, `{"cursor":"x"}`, agent.ErrInvalidInput, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg, pv := deadlinesWorld(t, tc.actorCaps)
			p := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-dl", ActingFor: clerk, Grant: tc.grant}
			res, err := reg.Call(context.Background(), p, agent.ToolListDeadlines, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			// rule: ARC-I-038
			if err := agenttools.CheckCitations(res); err != nil {
				t.Fatalf("citations: %v", err)
			}
			var out struct {
				Deadlines []struct {
					MatterID string `json:"matter_id"`
					Due      string `json:"due_date"`
				} `json:"deadlines"`
				NextCursor string `json:"next_cursor"`
			}
			if err := json.Unmarshal(res.Output, &out); err != nil {
				t.Fatalf("output: %v", err)
			}
			var due []string
			for _, d := range out.Deadlines {
				if d.MatterID != dlOpenMatter {
					t.Errorf("deadline of matter %q returned", d.MatterID)
				}
				due = append(due, d.Due)
			}
			if !slices.Equal(due, tc.wantDue) || out.NextCursor != tc.wantNext {
				t.Errorf("due = %v next %q, want %v next %q", due, out.NextCursor, tc.wantDue, tc.wantNext)
			}
			// rule: ARC-U-027
			// Returned due dates are sourced for a later write in the same run.
			for _, d := range due {
				if err := pv.Check("run-dl", []string{"/due"}, json.RawMessage(`{"due":"`+d+`"}`)); err != nil {
					t.Errorf("due %s not in provenance: %v", d, err)
				}
			}
			if err := pv.Check("run-dl", []string{"/due"}, json.RawMessage(`{"due":"2026-03-21"}`)); !errors.Is(err, agenttools.ErrUnsourcedValue) {
				t.Errorf("walled due date in provenance: %v", err)
			}
		})
	}
}
