package matter_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/work"
)

// mateProposedTasks starts an agent-proposed task in the default workflow's proposed status.
type mateProposedTasks struct{}

func (mateProposedTasks) ProposedTaskStatus(context.Context) (string, string, error) {
	return "default", "proposed", nil
}

// mateTaskHandlers is the app's handler set for def's tools, with create_task built over the
// seed's work items and the proposed status above (boot wires it from work configuration).
func mateTaskHandlers(t *testing.T, def feature.Def) agent.HandlerSet {
	t.Helper()
	var hs []agent.Handler
	for _, name := range def.Tools {
		h, ok := agent.HandlerFor(name)
		if !ok {
			continue
		}
		if name == agent.ToolCreateTask {
			h = agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
				can, err := agent.DepOf[agent.CapabilityCheck](d)
				if err != nil {
					return registry.Tool{}, err
				}
				walls, err := agent.DepOf[roles.Walls](d)
				if err != nil {
					return registry.Tool{}, err
				}
				items, err := agent.DepOf[agent.TaskCreator](d)
				if err != nil {
					return registry.Tool{}, err
				}
				return agent.CreateTaskHandler().Build(agent.NewDeps(can, walls, items, agent.ProposedTasks(mateProposedTasks{}), agenttools.NewProvenance()))
			}}
		}
		hs = append(hs, h)
	}
	s, err := agent.NewHandlerSet(hs...)
	if err != nil {
		t.Fatalf("handler set: %v", err)
	}
	return s
}

// rule: AI-R-003
// rule: ARC-I-035
func TestFMAT17_DefMatchesSpec(t *testing.T) {
	mateCheckDefMatchesSpec(t, matter.OpeningPlanDef())
}

func TestFMAT17_Prompt(t *testing.T) {
	mateCheckPrompt(t, matter.OpeningPlanDef())
}

// F-MAT-17 starts only on request from the matter-open wizard.
func TestFMAT17_TriggerBinding(t *testing.T) {
	mateCheckTrigger(t, matter.OpeningPlanDef(), "matter-open-wizard > Create folders, task plan and calendar placeholders")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFMAT17_FromFeature(t *testing.T) {
	cases := []struct {
		name   string
		inject bool
	}{
		{"ok", false},
		{"injection", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			def := matter.OpeningPlanDef()
			fd := agenttest.FromFeatureWith(t, def, mateTaskHandlers(t, def))
			if len(fd.StandIns) != 0 {
				t.Fatalf("stand-ins = %v, want none", fd.StandIns)
			}
			m := fd.Seed.Matter("2026-0317", "Synth v Example")
			mid := string(m.ID)
			lines := []string{"Engagement confirmed. Statement of claim to be served by 2026-11-16."}
			if c.inject {
				lines = append(lines, mateInjected)
			}
			doc, v := fd.Seed.Document(mid, lines)
			calls := []mateCall{
				{agent.ToolReportPlan, map[string]any{"steps": []map[string]any{{"step_id": "read", "title": "Read the opening documents"}, {"step_id": "plan", "title": "Propose the opening plan"}}}},
				{agent.ToolGetMatter, map[string]any{"matter_id": mid}},
				{agent.ToolSearchDocuments, map[string]any{"matter_id": mid, "query": "served by"}},
				{agent.ToolReadDocument, map[string]any{"doc_id": doc, "version_id": string(v)}},
				{agent.ToolSearchContacts, map[string]any{"query": "Synth"}},
				{agent.ToolCreateTask, map[string]any{"matter_id": mid, "title": "Set up the matter folders"}},
				// rule: ARC-U-027
				// The service date is only in quoted document text, not a date a read tool returned, so
				// the placeholder carries no due_on (create_task would refuse it as unsourced).
				{agent.ToolCreateTask, map[string]any{"matter_id": mid, "title": "Placeholder: serve the statement of claim (unconfirmed)"}},
			}
			model := mateScript(t, def.ID, "Proposed two opening tasks; the service date stays unconfirmed.", calls...)
			run := fd.Fire(ctx, model, "run-f-mat-17", mateScreenPrompt(def, "matter:"+mid))
			if run.Err != nil {
				t.Fatalf("run: %v", run.Err)
			}
			// rule: ARC-I-038
			// The script's calls ran in order and the run made no other call.
			want := []string{
				agent.ToolReportPlan, agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument,
				agent.ToolSearchContacts, agent.ToolCreateTask, agent.ToolCreateTask,
			}
			if !slices.Equal(run.Tools, want) || len(run.Refused) != 0 || len(run.StandIns) != 0 {
				t.Fatalf("tools = %v refused %v stand-ins %v, want %v", run.Tools, run.Refused, run.StandIns, want)
			}
			// rule: ARC-I-039
			if run.PlanAt != 0 || run.FirstWrite != 5 {
				t.Fatalf("plan at %d, first write %d; want report_plan before create_task", run.PlanAt, run.FirstWrite)
			}
			// rule: AI-R-005
			// Each task is a proposal in the proposed status, reported by the acting user; none
			// is accepted, and the agent cannot approve it.
			for _, i := range []int{5, 6} {
				var out agenttools.WriteOutput
				if err := json.Unmarshal([]byte(run.ToolResults[i].Text), &out); err != nil || out.Outcome != agenttools.OutcomeProposal {
					t.Fatalf("create_task result = %s, want a proposal", run.ToolResults[i].Text)
				}
				it, err := fd.Seed.Work.Get(ctx, work.ItemID(out.Ref))
				if err != nil || it.StatusID != "proposed" || it.ReporterID != agenttest.FeatureActingFor || it.Subject.ID != mid {
					t.Fatalf("task = %+v (%v), want proposed on the matter, reported by the acting user", it, err)
				}
				if c.inject && strings.Contains(it.Title, mateInjectedParty) {
					t.Fatalf("injected text in task %q", it.Title)
				}
				if err := fd.Seed.Work.SetApprover(ctx, nil, it.ID, "agent", true); !errors.Is(err, work.ErrSelfApproval) {
					t.Fatalf("agent as approver: %v, want ErrSelfApproval", err)
				}
				mateDecisions(t, matter.GateWorkPlan, registry.Subject{Kind: "work_item", ID: out.Ref})
			}
			if c.inject {
				mateDocQuotedOnly(t, run.Run, mateInjected)
				mateAbsentFromWrites(t, run.Run, agent.ToolCreateTask, agent.ToolSubmitForApproval)
			}
		})
	}
}
