package work_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
	kwork "github.com/dagi377/counsel/kernel/work"
)

// Shared test helpers of the work AI features. Every name carries the wrk prefix.
//
// The end-to-end tests drive a Def with agenttest.FromFeatureWith over the app's real
// handlers (agent.Handlers()) and no stand-ins. Beyond the Seed, wrkEnv gives create_task the
// proposed status boot wires from work configuration, gives submit_for_approval a subject
// lookup that resolves a work item to its matter, records every value a read tool returns in
// the run's provenance and holds create_task's due date to that provenance.

// wrkOtherLawyer is a second synthetic user who holds every gate kind's approver capability.
const wrkOtherLawyer = "user-synthetic-work-lawyer"

// wrkSubjectWorkItem is the subject type submit_for_approval names for a work item.
const wrkSubjectWorkItem = "work_item"

// wrkDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var wrkDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// wrkProposedTasks starts an agent-proposed task in the default workflow's proposed status.
type wrkProposedTasks struct{}

func (wrkProposedTasks) ProposedTaskStatus(context.Context) (string, string, error) {
	return "default", "proposed", nil
}

// wrkItemGetter is the part of the Seed's work store the subject lookup reads.
type wrkItemGetter interface {
	Get(ctx context.Context, id kwork.ItemID) (kwork.Item, error)
}

// wrkSubjects implements agent.SubjectMatters: a matter is its own matter and a work item
// belongs to the matter it is on. Any other subject is not found.
type wrkSubjects struct{ items wrkItemGetter }

func (s wrkSubjects) SubjectMatter(ctx context.Context, subjectType, id string) (string, error) {
	switch subjectType {
	case roles.SubjectMatter:
		return id, nil
	case wrkSubjectWorkItem:
		it, err := s.items.Get(ctx, kwork.ItemID(id))
		if err != nil {
			return "", fmt.Errorf("work item %s: %w", id, agenttools.ErrNotFound)
		}
		return it.Subject.ID, nil
	}
	return "", fmt.Errorf("subject %s %s: %w", subjectType, id, agenttools.ErrNotFound)
}

// wrkDep appends d's value of type T to vals, when d has one.
func wrkDep[T any](vals []any, d agent.Deps) []any {
	if v, err := agent.DepOf[T](d); err == nil {
		return append(vals, v)
	}
	return vals
}

// wrkHandlers is agent.Handlers() with each Build given the Seed's ports, wrkProposedTasks and
// wrkSubjects. Every read tool records its values in pv, and create_task's due date must be
// one of them (ARC-U-027).
func wrkHandlers(t *testing.T, pv *agenttools.Provenance) agent.HandlerSet {
	t.Helper()
	app := agent.Handlers()
	var hs []agent.Handler
	for _, name := range app.Names() {
		h, _ := app.For(name)
		build := h.Build
		hs = append(hs, agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
			vals := []any{agent.ProposedTasks(wrkProposedTasks{}), pv}
			if list, err := agent.DepOf[agent.WorkList](d); err == nil {
				if items, ok := list.(wrkItemGetter); ok {
					vals = append(vals, agent.SubjectMatters(wrkSubjects{items: items}))
				}
			}
			vals = wrkDep[agent.CapabilityCheck](vals, d)
			vals = wrkDep[roles.Walls](vals, d)
			vals = wrkDep[agenttools.Plans](vals, d)
			vals = wrkDep[agenttools.GateOpener](vals, d)
			vals = wrkDep[agenttools.RunScopes](vals, d)
			vals = wrkDep[agent.DocumentText](vals, d)
			vals = wrkDep[agent.WorkList](vals, d)
			vals = wrkDep[agent.ClientMatters](vals, d)
			vals = wrkDep[matter.Query](vals, d)
			vals = wrkDep[agent.ContactSearch](vals, d)
			vals = wrkDep[*agenttest.SeedStore](vals, d)
			tool, err := build(agent.NewDeps(vals...))
			switch {
			case err != nil:
				return tool, err
			case tool.Kind == registry.ToolRead:
				return pv.ReadTool(tool)
			case tool.Name == agent.ToolCreateTask:
				return pv.SourcedWrite(tool, "/due_on")
			}
			return tool, nil
		}})
	}
	set, err := agent.NewHandlerSet(hs...)
	if err != nil {
		t.Fatalf("handler set: %v", err)
	}
	return set
}

// wrkWorld is one seeded run: the driver, the matter, its open work item and its document.
type wrkWorld struct {
	fd      *agenttest.FeatureDriver
	matter  string
	item    string
	doc     string
	version string
}

// wrkFeature builds def's FeatureDriver over the real handlers with an approvable gate store,
// and seeds one matter with one open work item (itemTitle) and one document of lines.
func wrkFeature(t *testing.T, def feature.Def, itemTitle string, lines ...string) wrkWorld {
	t.Helper()
	fd := agenttest.FromFeatureWith(t, def, wrkHandlers(t, agenttools.NewProvenance()))
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, wrkOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return wrkDecideAt }, caps)
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	m := fd.Seed.Matter("2026-0401", "Synth v Example")
	item := fd.Seed.WorkItem(string(m.ID), itemTitle)
	doc, v := fd.Seed.Document(string(m.ID), lines)
	return wrkWorld{fd: fd, matter: string(m.ID), item: string(item), doc: doc, version: string(v)}
}

// wrkCall is one scripted model tool call.
type wrkCall struct {
	name  string
	input map[string]any
}

// wrkReads are the read calls every work script makes on w, in order.
func wrkReads(w wrkWorld) []wrkCall {
	return []wrkCall{
		{agent.ToolGetMatter, map[string]any{"matter_id": w.matter}},
		{agent.ToolListWorkItems, map[string]any{"matter_id": w.matter}},
		{agent.ToolListDocuments, map[string]any{"matter_id": w.matter}},
		{agent.ToolReadDocument, map[string]any{"doc_id": w.doc, "version_id": w.version}},
	}
}

// wrkPlan is a report_plan call over steps.
func wrkPlan(steps ...string) wrkCall {
	var s []map[string]any
	for _, st := range steps {
		s = append(s, map[string]any{"step_id": st, "title": "Step " + st})
	}
	return wrkCall{agenttools.ToolReportPlan, map[string]any{"steps": s}}
}

// wrkSubmit is a submit_for_approval call opening kind on work item ref.
func wrkSubmit(ref, kind string) wrkCall {
	return wrkCall{agent.ToolSubmitForApproval, map[string]any{"subject_type": wrkSubjectWorkItem, "subject_id": ref, "gate_kind": kind}}
}

// wrkScript is an inline aifake model for featureID: one tool call per turn, then text.
func wrkScript(t *testing.T, featureID string, calls []wrkCall, text string) *aifake.Fake {
	t.Helper()
	var turns []map[string]any
	for _, c := range calls {
		turns = append(turns, map[string]any{"reply": map[string]any{
			"tool_calls": []map[string]any{{"name": c.name, "input": c.input}}, "stop_reason": "tool_use",
		}})
	}
	turns = append(turns, map[string]any{"reply": map[string]any{"text": text, "stop_reason": "end_turn"}})
	b, err := json.Marshal(map[string]any{
		"synthetic": true, "feature": featureID, "match": map[string]any{"last_user_regex": "."}, "turns": turns,
	})
	if err != nil {
		t.Fatal(err)
	}
	return aifake.New(fstest.MapFS{strings.ToLower(featureID) + "/inline/ok.yaml": {Data: b}})
}

// wrkToolNames lists the tool names of calls.
func wrkToolNames(calls []wrkCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// wrkCheckTask fails t unless the i-th tool result of run is a create_task write of outcome
// and its task stays proposed on w's matter, reported by the acting user, with no approver:
// the agent never accepts its own work. It returns the task's id.
func wrkCheckTask(t *testing.T, w wrkWorld, run agenttest.FeatureRun, i int, outcome agenttools.WriteOutcome) string {
	t.Helper()
	var out agenttools.WriteOutput
	if err := json.Unmarshal([]byte(run.ToolResults[i].Text), &out); err != nil || out.Outcome != outcome {
		t.Fatalf("create_task result = %s, want a %s", run.ToolResults[i].Text, outcome)
	}
	it, err := w.fd.Seed.Work.Get(context.Background(), kwork.ItemID(out.Ref))
	if err != nil || it.StatusID != "proposed" || it.ApproverID != "" || it.ReporterID != agenttest.FeatureActingFor || it.Subject.ID != w.matter {
		t.Fatalf("task = %+v (%v), want proposed on %s, reported by %s, no approver", it, err, w.matter, agenttest.FeatureActingFor)
	}
	return out.Ref
}

// wrkCheckPlanned fails t unless run ended cleanly with no stand-in, made exactly calls and
// called report_plan before its first write, create_task (ARC-I-039).
func wrkCheckPlanned(t *testing.T, w wrkWorld, run agenttest.FeatureRun, calls []wrkCall) {
	t.Helper()
	if run.Err != nil {
		t.Fatalf("run: %v", run.Err)
	}
	// rule: ARC-I-035
	if len(w.fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want none", w.fd.StandIns, run.StandIns)
	}
	if want := wrkToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	// rule: ARC-I-039
	if run.PlanAt < 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != agent.ToolCreateTask {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before create_task", run.PlanAt, run.FirstWrite, run.Tools)
	}
	read := slices.Index(run.Tools, agent.ToolReadDocument)
	if read < 0 || !strings.Contains(run.ToolResults[read].Text, w.version) {
		t.Fatalf("read_document result = %v, want the seeded version %s", run.ToolResults, w.version)
	}
}

// wrkDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func wrkDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: wrkDecideAt.Add(-time.Minute),
	}, by)
}

// wrkStatus is the HTTP status the API returns for a gate decision error.
func wrkStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// wrkCheckGate fails t unless gate id is pending, of kind, prepared for the acting user, and
// neither an agent principal (403) nor the acting user (ErrSelfApproval, 403) can decide it
// while another lawyer can.
func wrkCheckGate(t *testing.T, fd *agenttest.FeatureDriver, id, kind string) {
	t.Helper()
	ctx := context.Background()
	g, err := fd.Seed.Gates.Get(ctx, gate.GateID(id))
	if err != nil {
		t.Fatal(err)
	}
	// rule: AI-R-003
	if g.Kind != kind || g.Status != gate.StatusPending || (g.Preparer.ActingFor != agenttest.FeatureActingFor && g.Preparer.UserID != agenttest.FeatureActingFor) {
		t.Fatalf("gate = %s %s preparer %+v; want pending %s prepared for %s", g.Kind, g.Status, g.Preparer, kind, agenttest.FeatureActingFor)
	}
	tests := []struct {
		name       string
		by         registry.Principal
		wantErr    error
		wantStatus int
	}{
		{"agent principal", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-" + id, ActingFor: agenttest.FeatureActingFor}, gate.ErrAgentApprover, http.StatusForbidden},
		{"acting user decides own gate", registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, gate.ErrSelfApproval, http.StatusForbidden},
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: wrkOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := wrkDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || wrkStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, wrkStatus(err), tt.wantErr, wrkStatus(tt.wantErr))
			}
		})
	}
}

// wrkCheckInjection fails t unless the injected text reached the model only as quoted data
// and is absent from every write input of run and every gate payload it opened (ARC-I-038).
func wrkCheckInjection(t *testing.T, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, injected string) {
	t.Helper()
	read := slices.Index(run.Tools, agent.ToolReadDocument)
	if read < 0 || !strings.Contains(run.ToolResults[read].Text, injected) {
		t.Fatalf("read_document result lacks the seeded text")
	}
	if err := agenttest.QuotedOnly(run.Run, injected); err != nil {
		t.Fatal(err)
	}
	writes := map[string]bool{}
	for _, s := range fd.Feature.Tools {
		writes[s.Name] = s.Kind == agent.Write
	}
	for _, req := range run.Requests {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Type == "tool_use" && writes[b.ToolName] && strings.Contains(string(b.Input), injected) {
					t.Fatalf("write input carries the injected text: %s", b.Input)
				}
			}
		}
	}
	for _, id := range run.GateIDs {
		g, err := fd.Seed.Gates.Get(context.Background(), gate.GateID(id))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(g.Payload), injected) {
			t.Fatalf("gate %s payload carries the injected text", id)
		}
	}
}

// wrkFeatureSpec is one spec/ai/features.json feature.
type wrkFeatureSpec struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Source    string   `json:"source"`
	Trigger   string   `json:"trigger"`
	OnRequest []string `json:"on_request"`
	Outputs   string   `json:"outputs"`
	Tools     []string `json:"tools"`
	ModelCall string   `json:"model_call"`
	Fixture   string   `json:"fixture"`
	Gate      struct {
		Kind []string `json:"kind"`
	} `json:"gate"`
}

// wrkSpec returns id's spec/ai/features.json entry.
func wrkSpec(t *testing.T, id string) wrkFeatureSpec {
	t.Helper()
	var spec struct {
		Features []wrkFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f wrkFeatureSpec) bool { return f.ID == id })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", id)
	}
	return spec.Features[i]
}

// wrkCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry (the spec
// sync check for this id), a feature Set takes it once and its run scope is its gate kinds.
func wrkCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	sf := wrkSpec(t, d.ID)
	sorted := func(s []string) string { s = slices.Clone(s); slices.Sort(s); return strings.Join(s, ",") }
	kind := d.Kind
	if kind == "" {
		kind = ai.KindToolUse
	}
	tests := []struct {
		field     string
		got, want string
	}{
		{"name", d.Name, sf.Name},
		{"area", d.Area, "work"},
		{"source", string(d.Source), sf.Source},
		{"trigger", d.Trigger, sf.Trigger},
		{"on request", sorted(d.Actions), sorted(sf.OnRequest)},
		{"tools", sorted(d.Tools), sorted(sf.Tools)},
		{"gate kinds", sorted(d.GateKinds), sorted(sf.Gate.Kind)},
		{"model call", string(kind), sf.ModelCall},
		{"fixture", d.Fixture, sf.Fixture},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, spec %q", tt.field, tt.got, tt.want)
			}
		})
	}
	s := feature.NewSet()
	if err := s.Add(d); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(d); !errors.Is(err, feature.ErrBadDef) {
		t.Fatalf("second Add = %v, want ErrBadDef", err)
	}
	rs, err := d.Spec()
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	// rule: AI-R-003
	// The run scope lets the run open only the Def's gate kinds.
	if !slices.Equal(rs.GateKinds, d.GateKinds) {
		t.Fatalf("run gate kinds = %v, want %v", rs.GateKinds, d.GateKinds)
	}
}

// wrkCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble and
// the shared rules, and names the task and the expected output from features.json plus every
// phrase in want.
func wrkCheckPrompt(t *testing.T, d feature.Def, want ...string) {
	t.Helper()
	sf := wrkSpec(t, d.ID)
	names, err := feature.FixtureNames(os.DirFS("../../.."), "testdata/ai")
	if err != nil {
		t.Fatalf("FixtureNames: %v", err)
	}
	if err := feature.LintPrompt(d, names); err != nil {
		t.Fatalf("LintPrompt: %v", err)
	}
	spec, err := d.Spec()
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if !strings.HasPrefix(spec.System, feature.Preamble(d.ID)+feature.Rules()) {
		t.Fatalf("system prompt does not start with the preamble and the shared rules")
	}
	for _, w := range append([]string{sf.Name, sf.Outputs, "report_plan"}, want...) {
		if !strings.Contains(spec.System, w) {
			t.Errorf("system prompt lacks %q", w)
		}
	}
}

// wrkCheckBinding fails t unless b binds d to events, run for the event's user with no
// schedule, and d runs on request from exactly actions.
func wrkCheckBinding(t *testing.T, b trigger.Binding, err error, d feature.Def, events, actions []string) {
	t.Helper()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"feature", b.FeatureID, d.ID},
		{"events", strings.Join(b.Events, ","), strings.Join(events, ",")},
		{"cadence", b.Cadence, trigger.CadenceNone},
		{"acting for", b.ActingFor, ""},
		{"actions", strings.Join(d.Actions, ","), strings.Join(actions, ",")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}
