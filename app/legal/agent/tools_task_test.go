package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/work"
	"github.com/dagi377/counsel/kernel/work/workfake"
)

const (
	taskMatter   = "91111111-1111-4111-8111-111111111111"
	taskWalled   = "92222222-2222-4222-8222-222222222222"
	taskAssignee = "93333333-3333-4333-8333-333333333333"
	taskOutsider = "94444444-4444-4444-8444-444444444444"
	taskWorkflow = "wf-task"
	taskProposed = "st-proposed"
	taskRun      = "run-task"
	taskNoPlan   = "run-task-noplan"
	taskCap      = "work.edit"
	taskDue      = "2026-10-20"
)

type proposedTasks struct{}

func (proposedTasks) ProposedTaskStatus(context.Context) (string, string, error) {
	return taskWorkflow, taskProposed, nil
}

type taskWorld struct {
	reg   *agenttools.Registry
	items *workfake.Fake
	pv    *agenttools.Provenance
}

// newTaskWorld registers the real create_task on a Registry over module fakes. taskRun has a
// plan; taskNoPlan has none. clerk holds actorCaps and is walled from taskWalled; taskOutsider
// is walled from taskMatter.
func newTaskWorld(t *testing.T, actorCaps map[string]bool) taskWorld {
	t.Helper()
	ctx := context.Background()
	walls := rolesfake.New()
	walls.Wall(clerk, taskWalled)
	walls.Wall(taskOutsider, taskMatter)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	w := taskWorld{items: workfake.New(), pv: agenttools.NewProvenance()}
	// rule: ARC-U-027
	// taskRun read taskDue, so its agent writes may carry it as a due date.
	w.pv.Observe(taskRun, json.RawMessage(`{"due_date":"`+taskDue+`"}`))
	tool, err := agent.CreateTaskHandler().Build(agent.NewDeps(can, walls, w.items, proposedTasks{}, w.pv))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, taskRun, []string{"propose a task"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	w.reg = agenttools.NewRegistry(plans)
	if err := w.reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return w
}

func taskInput(fields map[string]string) string {
	b, _ := json.Marshal(fields)
	return string(b)
}

// rule: ARC-I-035
// rule: ARC-I-036
// rule: ARC-I-039
func TestARC_I_035_CreateTask(t *testing.T) {
	edit := map[string]bool{taskCap: true}
	grant := []string{taskCap}
	agentP := func(run string, g []string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: clerk, Grant: g}
	}
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}
	onMatter := taskInput(map[string]string{"title": "Call opposing counsel", "matter_id": taskMatter, "assignee_id": taskAssignee, "due_on": taskDue})
	cases := []struct {
		name        string
		p           registry.Principal
		actorCaps   map[string]bool
		in          string
		wantErr     error
		wantSubject registry.Subject
		wantDue     bool
	}{
		{"agent proposes a matter task", agentP(taskRun, grant), edit, onMatter, nil, registry.Subject{Kind: agent.WorkSubjectMatter, ID: taskMatter}, true},
		{"agent proposes a task with no matter", agentP(taskRun, grant), edit, taskInput(map[string]string{"title": "Renew CPD"}), nil, registry.Subject{}, false},
		{"manual path", user, edit, onMatter, nil, registry.Subject{Kind: agent.WorkSubjectMatter, ID: taskMatter}, true},
		{"walled matter", agentP(taskRun, grant), edit, taskInput(map[string]string{"title": "x", "matter_id": taskWalled}), agent.ErrNotVisible, registry.Subject{}, false},
		{"walled matter on the manual path", user, edit, taskInput(map[string]string{"title": "x", "matter_id": taskWalled}), agent.ErrNotVisible, registry.Subject{}, false},
		{"assignee walled from the matter", agentP(taskRun, grant), edit,
			taskInput(map[string]string{"title": "x", "matter_id": taskMatter, "assignee_id": taskOutsider}), agent.ErrNotVisible, registry.Subject{}, false},
		{"run without the capability", agentP(taskRun, nil), edit, onMatter, agenttools.ErrMissingCapability, registry.Subject{}, false},
		{"acting user without the capability", agentP(taskRun, grant), map[string]bool{}, onMatter, agent.ErrForbidden, registry.Subject{}, false},
		{"no report_plan in the run", agentP(taskNoPlan, grant), edit, onMatter, agenttools.ErrPlanRequired, registry.Subject{}, false},
		{"title missing", agentP(taskRun, grant), edit, taskInput(map[string]string{"matter_id": taskMatter}), agenttools.ErrInvalidInput, registry.Subject{}, false},
		{"approver in the input", agentP(taskRun, grant), edit, taskInput(map[string]string{"title": "x", "approver_id": taskAssignee}), agenttools.ErrInvalidInput, registry.Subject{}, false},
		{"status in the input", agentP(taskRun, grant), edit, taskInput(map[string]string{"title": "x", "status": "done"}), agenttools.ErrInvalidInput, registry.Subject{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newTaskWorld(t, tc.actorCaps)
			res, err := w.reg.Call(ctx, tc.p, agent.ToolCreateTask, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				if items, _ := w.items.List(ctx, work.Filter{}); len(items) != 0 {
					t.Errorf("refused call created %d items", len(items))
				}
				return
			}
			// rule: ARC-I-035
			if err := agenttools.CheckWriteResult(res); err != nil {
				t.Fatalf("write result: %v", err)
			}
			var out agenttools.WriteOutput
			if err := json.Unmarshal(res.Output, &out); err != nil || out.Outcome != agenttools.OutcomeProposal || res.GateID != "" {
				t.Fatalf("output = %s gate %q, want a proposal", res.Output, res.GateID)
			}
			it, err := w.items.Get(ctx, work.ItemID(out.Ref))
			if err != nil {
				t.Fatalf("item: %v", err)
			}
			if it.Type != agent.WorkTypeTask || it.WorkflowID != taskWorkflow || it.StatusID != taskProposed ||
				it.ReporterID != clerk || it.ApproverID != "" || it.AssigneeIsAgent || it.Subject != tc.wantSubject || (it.DueAt != nil) != tc.wantDue {
				t.Errorf("item = %+v", it)
			}
		})
	}
}

// rule: ARC-I-039
// The proposed task is accepted by a person: an agent is never its approver.
func TestARC_I_039_CreateTaskNotApprovedByAgent(t *testing.T) {
	cases := []struct {
		name    string
		by      string
		isAgent bool
		wantErr error
	}{
		{"agent approves", clerk, true, work.ErrSelfApproval},
		{"assignee approves", taskAssignee, false, work.ErrSelfApproval},
		{"another lawyer approves", "user-lawyer", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newTaskWorld(t, map[string]bool{taskCap: true})
			p := registry.Principal{Kind: registry.PrincipalAgent, RunID: taskRun, ActingFor: clerk, Grant: []string{taskCap}}
			res, err := w.reg.Call(ctx, p, agent.ToolCreateTask, json.RawMessage(taskInput(map[string]string{"title": "x", "assignee_id": taskAssignee})))
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			var out agenttools.WriteOutput
			if err := json.Unmarshal(res.Output, &out); err != nil {
				t.Fatalf("output: %v", err)
			}
			err = w.items.SetApprover(ctx, nil, work.ItemID(out.Ref), tc.by, tc.isAgent)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("set approver err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// rule: ARC-I-035
// create_task stores description_markdown on the proposed work item; a blank one is none.
func TestCreateTaskDescription(t *testing.T) {
	p := registry.Principal{Kind: registry.PrincipalAgent, RunID: taskRun, ActingFor: clerk, Grant: []string{taskCap}}
	cases := []struct {
		name    string
		desc    string
		want    string // "" means no description stored
		wantErr error
	}{
		{name: "markdown stored", desc: "See **para 4** of the claim.", want: "See **para 4** of the claim."},
		{name: "absent", desc: ""},
		{name: "at the schema limit", desc: strings.Repeat("a", 10000), want: strings.Repeat("a", 10000)},
		{name: "over the schema limit", desc: strings.Repeat("a", 10001), wantErr: agenttools.ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newTaskWorld(t, map[string]bool{taskCap: true})
			fields := map[string]string{"title": "Draft reply"}
			if tc.desc != "" {
				fields["description_markdown"] = tc.desc
			}
			res, err := w.reg.Call(ctx, p, agent.ToolCreateTask, json.RawMessage(taskInput(fields)))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			var out agenttools.WriteOutput
			if err := json.Unmarshal(res.Output, &out); err != nil {
				t.Fatalf("output: %v", err)
			}
			it, err := w.items.Get(ctx, work.ItemID(out.Ref))
			if err != nil {
				t.Fatalf("item: %v", err)
			}
			if (it.Description == nil) != (tc.want == "") || (it.Description != nil && *it.Description != tc.want) {
				t.Fatalf("description = %v, want %q", it.Description, tc.want)
			}
		})
	}
}

// rule: AI-R-003
// rule: ARC-I-035
// An agent run whose feature declares a gate kind on work items gets a draft of the run, which
// submit_for_approval may open that gate on; every other caller gets a proposal. An agent's
// proposal in its own run scope is recorded as a draft of the run too, so the run may submit
// it under its feature's gate kind; nothing outside the run records anything.
func TestAI_R_003_CreateTaskDraft(t *testing.T) {
	agentP := registry.Principal{Kind: registry.PrincipalAgent, RunID: taskRun, ActingFor: clerk, Grant: []string{taskCap}}
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}
	cases := []struct {
		name     string
		p        registry.Principal
		scopeRun string
		scope    []string // gate kinds of the run scope; nil means no scope in ctx
		want     agenttools.WriteOutcome
		recorded bool // recorded as a draft of the run
	}{
		{"feature declares work.absence", agentP, taskRun, []string{"work.absence"}, agenttools.OutcomeDraft, true},
		{"feature declares work.output", agentP, taskRun, []string{"work.output"}, agenttools.OutcomeDraft, true},
		{"feature declares no work item kind", agentP, taskRun, []string{"cal.confirm"}, agenttools.OutcomeProposal, true},
		{"no run scope", agentP, "", nil, agenttools.OutcomeProposal, false},
		{"scope of another run", agentP, "run-other", []string{"work.absence"}, agenttools.OutcomeProposal, false},
		{"manual path", user, taskRun, []string{"work.absence"}, agenttools.OutcomeProposal, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			var scope *agenttools.Scope
			if tc.scope != nil {
				scope = agenttools.NewScope(tc.scopeRun, tc.scope)
				ctx = agenttools.WithScope(ctx, scope)
			}
			w := newTaskWorld(t, map[string]bool{taskCap: true})
			res, err := w.reg.Call(ctx, tc.p, agent.ToolCreateTask, json.RawMessage(taskInput(map[string]string{"title": "Cover the deadline", "assignee_id": taskAssignee})))
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if err := agenttools.CheckWriteResult(res); err != nil {
				t.Fatalf("write result: %v", err)
			}
			var out agenttools.WriteOutput
			if err := json.Unmarshal(res.Output, &out); err != nil || out.Outcome != tc.want || res.GateID != "" {
				t.Fatalf("output = %s gate %q, want %s", res.Output, res.GateID, tc.want)
			}
			// The draft is recorded in the run, so submit_for_approval can find it.
			if recorded := scope != nil && scope.HasDraft(out.Ref); recorded != tc.recorded {
				t.Fatalf("draft recorded = %v, want %v", recorded, tc.recorded)
			}
			// rule: ARC-I-039
			// A draft is still a task nobody accepted: no approver, in the proposed status.
			it, err := w.items.Get(ctx, work.ItemID(out.Ref))
			if err != nil || it.StatusID != taskProposed || it.ApproverID != "" {
				t.Fatalf("item = %+v (%v), want proposed with no approver", it, err)
			}
		})
	}
}
