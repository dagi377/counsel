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
	"sync"
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
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
	kwork "github.com/dagi377/counsel/kernel/work"
)

// Shared test helpers of the work AI features F-WRK-06, F-WRK-07 and F-WRK-09. Every name
// carries the wrkb prefix.
//
// The end-to-end tests drive a Def with agenttest.FromFeatureWith over the app's real
// handlers (agent.Handlers()) and no stand-ins. Beyond the Seed, wrkbHandlers gives
// create_task the proposed status boot wires from work configuration, gives draft_document a
// draft store, gives submit_for_approval a subject lookup that resolves a work item or a
// draft to its matter, records every value a read tool returns in the run's provenance and
// holds create_task's due date to that provenance (ARC-U-027).

// wrkbOtherLawyer is a second synthetic user who holds every gate kind's approver capability.
const wrkbOtherLawyer = "user-synthetic-work-reviewer"

// Subject types submit_for_approval names: a work item create_task made, or a draft
// document draft_document made.
const (
	wrkbSubjectWorkItem = "work_item"
	wrkbSubjectDocument = "document"
)

// wrkbDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var wrkbDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// wrkbProposedTasks starts an agent-proposed task in the default workflow's proposed status.
type wrkbProposedTasks struct{}

func (wrkbProposedTasks) ProposedTaskStatus(context.Context) (string, string, error) {
	return "default", "proposed", nil
}

// wrkbDraftID is the id of the n-th draft document a run stores.
func wrkbDraftID(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-00000000b%03d", n) }

// wrkbDrafts stores draft documents with deterministic uuid ids and maps each to its matter.
type wrkbDrafts struct {
	mu      sync.Mutex
	matters map[string]string
	drafts  []agent.NewDraftDocument
}

// CreateDraft implements agent.DocumentDrafts.
func (s *wrkbDrafts) CreateDraft(_ context.Context, d agent.NewDraftDocument) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drafts = append(s.drafts, d)
	id := wrkbDraftID(len(s.drafts))
	s.matters[id] = d.MatterID
	return id, nil
}

// all returns the drafts stored so far.
func (s *wrkbDrafts) all() []agent.NewDraftDocument {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.drafts)
}

// wrkbItemGetter is the part of the Seed's work store the subject lookup reads.
type wrkbItemGetter interface {
	Get(ctx context.Context, id kwork.ItemID) (kwork.Item, error)
}

// wrkbSubjects implements agent.SubjectMatters: a matter is its own matter, a work item
// belongs to the matter it is on and a draft to the matter it was drafted on. Any other
// subject is not found.
type wrkbSubjects struct {
	items  wrkbItemGetter
	drafts *wrkbDrafts
}

func (s wrkbSubjects) SubjectMatter(ctx context.Context, subjectType, id string) (string, error) {
	switch subjectType {
	case roles.SubjectMatter:
		return id, nil
	case wrkbSubjectWorkItem:
		if s.items == nil {
			break
		}
		it, err := s.items.Get(ctx, kwork.ItemID(id))
		if err != nil {
			return "", fmt.Errorf("work item %s: %w", id, agenttools.ErrNotFound)
		}
		return it.Subject.ID, nil
	case wrkbSubjectDocument:
		s.drafts.mu.Lock()
		m, ok := s.drafts.matters[id]
		s.drafts.mu.Unlock()
		if ok {
			return m, nil
		}
	}
	return "", fmt.Errorf("subject %s %s: %w", subjectType, id, agenttools.ErrNotFound)
}

// wrkbDep appends d's value of type T to vals, when d has one.
func wrkbDep[T any](vals []any, d agent.Deps) []any {
	if v, err := agent.DepOf[T](d); err == nil {
		return append(vals, v)
	}
	return vals
}

// wrkbHandlers is agent.Handlers() with each Build given the Seed's ports plus
// wrkbProposedTasks, drafts and wrkbSubjects. Every read tool records its values in pv, and
// create_task's due date must be one of them (ARC-U-027).
func wrkbHandlers(t *testing.T, pv *agenttools.Provenance, drafts *wrkbDrafts) agent.HandlerSet {
	t.Helper()
	app := agent.Handlers()
	var hs []agent.Handler
	for _, name := range app.Names() {
		h, _ := app.For(name)
		build := h.Build
		hs = append(hs, agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
			vals := []any{agent.ProposedTasks(wrkbProposedTasks{}), agent.DocumentDrafts(drafts), pv}
			subjects := wrkbSubjects{drafts: drafts}
			if list, err := agent.DepOf[agent.WorkList](d); err == nil {
				if items, ok := list.(wrkbItemGetter); ok {
					subjects.items = items
				}
			}
			vals = append(vals, agent.SubjectMatters(subjects))
			vals = wrkbDep[agent.CapabilityCheck](vals, d)
			vals = wrkbDep[roles.Walls](vals, d)
			vals = wrkbDep[agenttools.Plans](vals, d)
			vals = wrkbDep[agenttools.GateOpener](vals, d)
			vals = wrkbDep[agenttools.RunScopes](vals, d)
			vals = wrkbDep[agent.DocumentText](vals, d)
			vals = wrkbDep[agent.DocumentLookup](vals, d)
			vals = wrkbDep[agent.WorkList](vals, d)
			vals = wrkbDep[agent.ClientMatters](vals, d)
			vals = wrkbDep[matter.Query](vals, d)
			vals = wrkbDep[agent.ContactSearch](vals, d)
			vals = wrkbDep[*agenttest.SeedStore](vals, d)
			tool, err := build(agent.NewDeps(vals...))
			switch {
			case err != nil:
				return tool, err
			case tool.Kind == registry.ToolRead:
				return pv.ReadTool(tool)
			case tool.Name == agent.ToolCreateTask:
				// rule: ARC-I-035
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

// wrkbWorld is one seeded run: the driver, the matter, its open work item, its document and
// the draft store.
type wrkbWorld struct {
	fd      *agenttest.FeatureDriver
	drafts  *wrkbDrafts
	matter  string
	item    string
	doc     string
	version string
}

// wrkbFeature builds def's FeatureDriver over the real handlers with an approvable gate
// store, and seeds one matter with one open work item (itemTitle) and one document of lines.
func wrkbFeature(t *testing.T, def feature.Def, itemTitle string, lines ...string) wrkbWorld {
	t.Helper()
	drafts := &wrkbDrafts{matters: map[string]string{}}
	fd := agenttest.FromFeatureWith(t, def, wrkbHandlers(t, agenttools.NewProvenance(), drafts))
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, wrkbOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return wrkbDecideAt }, caps)
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	m := fd.Seed.Matter("2026-0406", "Synth v Example")
	item := fd.Seed.WorkItem(string(m.ID), itemTitle)
	doc, v := fd.Seed.Document(string(m.ID), lines)
	return wrkbWorld{fd: fd, drafts: drafts, matter: string(m.ID), item: string(item), doc: doc, version: string(v)}
}

// wrkbCall is one scripted model tool call.
type wrkbCall struct {
	name  string
	input map[string]any
}

// wrkbReads are the read calls every work script makes on w, in order.
func wrkbReads(w wrkbWorld) []wrkbCall {
	return []wrkbCall{
		{agent.ToolGetMatter, map[string]any{"matter_id": w.matter}},
		{agent.ToolListWorkItems, map[string]any{"matter_id": w.matter}},
		{agent.ToolListDocuments, map[string]any{"matter_id": w.matter}},
		{agent.ToolReadDocument, map[string]any{"doc_id": w.doc, "version_id": w.version}},
	}
}

// wrkbPlan is a report_plan call over steps.
func wrkbPlan(steps ...string) wrkbCall {
	var s []map[string]any
	for _, st := range steps {
		s = append(s, map[string]any{"step_id": st, "title": "Step " + st})
	}
	return wrkbCall{agenttools.ToolReportPlan, map[string]any{"steps": s}}
}

// wrkbSubmit is a submit_for_approval call opening kind on subject ref of subjectType.
func wrkbSubmit(subjectType, ref, kind string) wrkbCall {
	return wrkbCall{agent.ToolSubmitForApproval, map[string]any{"subject_type": subjectType, "subject_id": ref, "gate_kind": kind}}
}

// wrkbScript is an inline aifake model for featureID: one tool call per turn, then text.
func wrkbScript(t *testing.T, featureID string, calls []wrkbCall, text string) *aifake.Fake {
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

// wrkbToolNames lists the tool names of calls.
func wrkbToolNames(calls []wrkbCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// wrkbCheckPlanned fails t unless run ended cleanly with no stand-in, made exactly calls,
// read the seeded document version and called report_plan before its first write, firstWrite
// (ARC-I-039).
func wrkbCheckPlanned(t *testing.T, w wrkbWorld, run agenttest.FeatureRun, calls []wrkbCall, firstWrite string) {
	t.Helper()
	if run.Err != nil {
		t.Fatalf("run: %v", run.Err)
	}
	// rule: ARC-I-035
	if len(w.fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want none", w.fd.StandIns, run.StandIns)
	}
	if want := wrkbToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	// rule: ARC-I-039
	if run.PlanAt < 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != firstWrite {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before %s", run.PlanAt, run.FirstWrite, run.Tools, firstWrite)
	}
	read := slices.Index(run.Tools, agent.ToolReadDocument)
	if read < 0 || !strings.Contains(run.ToolResults[read].Text, w.version) {
		t.Fatalf("read_document result = %v, want the seeded version %s", run.ToolResults, w.version)
	}
}

// wrkbWriteOutput decodes the i-th tool result of run as a write output of outcome.
func wrkbWriteOutput(t *testing.T, run agenttest.FeatureRun, i int, outcome agenttools.WriteOutcome) agenttools.WriteOutput {
	t.Helper()
	var out agenttools.WriteOutput
	if err := json.Unmarshal([]byte(run.ToolResults[i].Text), &out); err != nil || out.Outcome != outcome {
		t.Fatalf("%s result = %s, want a %s", run.Tools[i], run.ToolResults[i].Text, outcome)
	}
	return out
}

// wrkbCheckTask fails t unless the i-th tool result of run is a create_task write of outcome
// and its task stays proposed on w's matter, reported by the acting user, with no approver:
// the agent never accepts its own work. It returns the task.
func wrkbCheckTask(t *testing.T, w wrkbWorld, run agenttest.FeatureRun, i int, outcome agenttools.WriteOutcome) kwork.Item {
	t.Helper()
	out := wrkbWriteOutput(t, run, i, outcome)
	it, err := w.fd.Seed.Work.Get(context.Background(), kwork.ItemID(out.Ref))
	// rule: AI-R-003
	if err != nil || it.StatusID != "proposed" || it.ApproverID != "" || it.ReporterID != agenttest.FeatureActingFor || it.Subject.ID != w.matter {
		t.Fatalf("task = %+v (%v), want proposed on %s, reported by %s, no approver", it, err, w.matter, agenttest.FeatureActingFor)
	}
	return it
}

// wrkbCheckRefusedSubmit fails t unless run refused only submit_for_approval, with want, and
// opened no gate.
func wrkbCheckRefusedSubmit(t *testing.T, run agenttest.FeatureRun, want error) {
	t.Helper()
	if len(run.Refused) != 1 || run.Refused[0].Tool != agent.ToolSubmitForApproval || !errors.Is(run.Refused[0].Err, want) {
		t.Fatalf("refused = %v, want submit_for_approval refused with %v", run.Refused, want)
	}
	if len(run.GateIDs) != 0 {
		t.Fatalf("gates = %v, want none", run.GateIDs)
	}
}

// wrkbCheckUnsourced fails t unless run refused create_task with ErrUnsourcedValue, created
// no task beyond the seeded item and opened no gate (ARC-U-027).
func wrkbCheckUnsourced(t *testing.T, w wrkbWorld, run agenttest.FeatureRun) {
	t.Helper()
	if run.Err != nil {
		t.Fatalf("run: %v", run.Err)
	}
	i := slices.IndexFunc(run.Refused, func(r agenttest.Refusal) bool { return r.Tool == agent.ToolCreateTask })
	if i < 0 || !errors.Is(run.Refused[i].Err, agenttools.ErrUnsourcedValue) {
		t.Fatalf("refused = %v, want create_task refused with ErrUnsourcedValue", run.Refused)
	}
	if len(run.GateIDs) != 0 {
		t.Fatalf("gates = %v, want none", run.GateIDs)
	}
	items, err := w.fd.Seed.Work.List(context.Background(), kwork.Filter{})
	if err != nil || len(items) != 1 {
		t.Fatalf("work items = %d (%v), want only the seeded item", len(items), err)
	}
}

// wrkbDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func wrkbDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: wrkbDecideAt.Add(-time.Minute),
	}, by)
}

// wrkbStatus is the HTTP status the API returns for a gate decision error.
func wrkbStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// wrkbCheckGate fails t unless run opened exactly one gate, pending, of kind, prepared for
// the acting user, that neither an agent principal (403) nor the acting user
// (ErrSelfApproval, 403) can decide while another lawyer can.
func wrkbCheckGate(t *testing.T, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, kind string) {
	t.Helper()
	if len(run.Refused) != 0 || len(run.GateIDs) != 1 {
		t.Fatalf("refused = %v, gates = %v; want none refused and one gate", run.Refused, run.GateIDs)
	}
	ctx := context.Background()
	id := run.GateIDs[0]
	g, err := fd.Seed.Gates.Get(ctx, gate.GateID(id))
	if err != nil {
		t.Fatal(err)
	}
	// rule: AI-R-003
	// rule: ARC-I-035
	if g.Kind != kind || g.Status != gate.StatusPending || g.Preparer.ActingFor != agenttest.FeatureActingFor {
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
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: wrkbOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := wrkbDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || wrkbStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, wrkbStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// wrkbCheckInjection fails t unless the injected text reached the model only as quoted data
// and is absent from every write input of run, every gate payload it opened and every draft
// it stored (ARC-I-038).
func wrkbCheckInjection(t *testing.T, w wrkbWorld, run agenttest.FeatureRun, injected string) {
	t.Helper()
	read := slices.Index(run.Tools, agent.ToolReadDocument)
	if read < 0 || !strings.Contains(run.ToolResults[read].Text, injected) {
		t.Fatalf("read_document result lacks the seeded text")
	}
	// rule: ARC-I-038
	if err := agenttest.QuotedOnly(run.Run, injected); err != nil {
		t.Fatal(err)
	}
	writes := map[string]bool{}
	for _, s := range w.fd.Feature.Tools {
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
		g, err := w.fd.Seed.Gates.Get(context.Background(), gate.GateID(id))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(g.Payload), injected) {
			t.Fatalf("gate %s payload carries the injected text", id)
		}
	}
	for _, d := range w.drafts.all() {
		if strings.Contains(d.Title+d.BodyMarkdown, injected) {
			t.Fatalf("draft %q carries the injected text", d.Title)
		}
	}
}

// wrkbFeatureSpec is one spec/ai/features.json feature.
type wrkbFeatureSpec struct {
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

// wrkbSpec returns id's spec/ai/features.json entry.
func wrkbSpec(t *testing.T, id string) wrkbFeatureSpec {
	t.Helper()
	var spec struct {
		Features []wrkbFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f wrkbFeatureSpec) bool { return f.ID == id })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", id)
	}
	return spec.Features[i]
}

// wrkbCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry (the spec
// sync check for this id), a feature Set takes it once and its run scope is its gate kinds.
func wrkbCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	sf := wrkbSpec(t, d.ID)
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
		{"on request", strings.Join(d.Actions, ";"), strings.Join(sf.OnRequest, ";")},
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

// wrkbCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble and
// the shared rules, and names the task and the expected output from features.json plus every
// phrase in want.
func wrkbCheckPrompt(t *testing.T, d feature.Def, want ...string) {
	t.Helper()
	sf := wrkbSpec(t, d.ID)
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

// wrkbCheckBinding fails t unless b binds d to events and cadence, acting for the event's
// user (designated false) or for the feature's designated lawyer (designated true), and d
// runs on request from exactly actions. A designated binding fails closed: with no
// designated lawyer in firm settings, readiness flags the feature as not startable.
func wrkbCheckBinding(t *testing.T, b trigger.Binding, err error, d feature.Def, events []string, cadence trigger.Cadence, designated bool, actions []string) {
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
		{"cadence", b.Cadence, cadence},
		{"designated", b.Designated, designated},
		{"acting for", b.ActingFor, ""},
		{"actions", strings.Join(d.Actions, ";"), strings.Join(actions, ";")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
	if !designated {
		return
	}
	s := trigger.New(nil, clockfake.New(wrkbDecideAt, time.UTC), nil)
	if err := s.Add(b); err != nil {
		t.Fatalf("Add: %v", err)
	}
	unready, err := s.Readiness(context.Background())
	want := trigger.Unready{FeatureID: b.FeatureID, Reason: trigger.ReasonNoDesignatedLawyer}
	if err != nil || len(unready) != 1 || unready[0] != want {
		t.Fatalf("readiness = %+v, %v; want %+v", unready, err, want)
	}
}
