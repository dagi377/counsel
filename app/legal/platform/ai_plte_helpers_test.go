package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// The plte helpers drive the platform features that draft on one matter (F-PLT-11, F-PLT-12)
// with agenttest.FromFeature over the app's real handlers, with no stand-ins. A run reads one
// seeded matter and the documents filed on it with Seed.Document, drafts on that matter with
// draft_document and opens the Def's gate kind on the draft with submit_for_approval.

// plteOtherLawyer is a second synthetic user who holds every gate kind's approver capability.
const plteOtherLawyer = "user-synthetic-plte-lawyer"

// plteDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var plteDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// plteDoc is one seeded document: its id and the version id read_document must be given.
type plteDoc struct {
	id      string
	version string
}

// plteWorld is one seeded run: the driver, the matter the run reads and its documents.
type plteWorld struct {
	fd       *agenttest.FeatureDriver
	matterID string
	clientID string
	docs     []plteDoc
}

// plteFeature builds def's FeatureDriver over the real handlers with a gate store in which
// the acting user and plteOtherLawyer hold every kind's approver capability, so the preparer
// rule, not a missing capability, is what stops the acting user. It seeds matter number with
// shortName and files one single-page document per entry of docs on it.
func plteFeature(t *testing.T, def feature.Def, number, shortName string, docs ...[]string) plteWorld {
	t.Helper()
	fd := agenttest.FromFeature(t, def)
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, plteOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return plteDecideAt }, caps)
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	m := fd.Seed.Matter(number, shortName)
	w := plteWorld{fd: fd, matterID: string(m.ID), clientID: string(m.ClientID)}
	for _, lines := range docs {
		id, v := fd.Seed.Document(w.matterID, lines)
		w.docs = append(w.docs, plteDoc{id: id, version: string(v)})
	}
	return w
}

// draftID is the id the Seed gives the run's first draft of kind ("document" or "email"):
// its store numbers documents and drafts in one sequence, after the seeded documents.
func (w plteWorld) draftID(kind string) string {
	return fmt.Sprintf("draft-%s-%d", kind, len(w.docs)+1)
}

// plteCall is one scripted model tool call.
type plteCall struct {
	name  string
	input string // JSON
}

// plteJSON marshals v for a script, failing t on error.
func plteJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// plteReads are the read calls a script makes on w: the matter, one search of its documents
// and a read of every seeded document at its version.
func plteReads(t *testing.T, w plteWorld, query string) []plteCall {
	t.Helper()
	calls := []plteCall{
		{agent.ToolGetMatter, plteJSON(t, map[string]string{"matter_id": w.matterID})},
		{agent.ToolSearchDocuments, plteJSON(t, map[string]string{"matter_id": w.matterID, "query": query})},
	}
	for _, d := range w.docs {
		calls = append(calls, plteCall{agent.ToolReadDocument, plteJSON(t, map[string]string{"doc_id": d.id, "version_id": d.version})})
	}
	return calls
}

// plteMatterWrites plans, drafts body on the run's matter and submits the draft under
// gateKind.
func plteMatterWrites(t *testing.T, w plteWorld, title, body, gateKind string) []plteCall {
	t.Helper()
	return []plteCall{
		{agenttools.ToolReportPlan, `{"steps": [{"step_id": "read", "title": "Read the records"}, {"step_id": "draft", "title": "Draft and send for approval"}]}`},
		{agent.ToolDraftDocument, plteJSON(t, map[string]string{"matter_id": w.matterID, "title": title, "body_markdown": body})},
		{agent.ToolSubmitForApproval, plteJSON(t, map[string]string{"subject_type": "document", "subject_id": w.draftID("document"), "gate_kind": gateKind})},
	}
}

// plteScript is an inline aifake fixture for feature: one tool call per turn, then text.
func plteScript(feature, dir string, calls []plteCall, text string) *aifake.Fake {
	var b strings.Builder
	fmt.Fprintf(&b, "synthetic: true\nfeature: %s\nmatch:\n  last_user_regex: '.'\nturns:\n", feature)
	for _, c := range calls {
		fmt.Fprintf(&b, "  - reply:\n      tool_calls:\n        - name: %s\n          input: %s\n      stop_reason: tool_use\n", c.name, c.input)
	}
	fmt.Fprintf(&b, "  - reply:\n      text: %q\n      stop_reason: end_turn\n", text)
	return aifake.New(fstest.MapFS{strings.ToLower(feature) + "/" + dir + "/ok.yaml": {Data: []byte(b.String())}})
}

// plteToolNames lists the tool names of calls.
func plteToolNames(calls []plteCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// plteCheckRun fails t unless run used no stand-in, ran every call, planned before its first
// write (draft_document), read every seeded document for real at its version, stored one
// draft on the run's matter prepared for the acting user and opened one gate of kind that
// only another lawyer can decide.
func plteCheckRun(t *testing.T, w plteWorld, run agenttest.FeatureRun, calls []plteCall, kind string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	// rule: ARC-I-035
	if len(w.fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want every tool on its real handler", w.fd.StandIns, run.StandIns)
	}
	if want := plteToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	// rule: ARC-I-039
	if run.PlanAt < 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != agent.ToolDraftDocument {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before draft_document", run.PlanAt, run.FirstWrite, run.Tools)
	}
	plteCheckReads(t, w, run)
	// The draft is on the matter the run read, stored for the acting user, never in the
	// agent's name.
	drafts := w.fd.Seed.Store.Drafts
	if len(drafts) != 1 || drafts[0].ID != w.draftID("document") || drafts[0].PreparedBy != agenttest.FeatureActingFor || drafts[0].MatterID != w.matterID {
		t.Fatalf("drafts = %+v, want one draft %s on %s prepared by %s", drafts, w.draftID("document"), w.matterID, agenttest.FeatureActingFor)
	}
	plteCheckGate(t, w.fd, run, kind)
}

// plteCheckReads fails t unless read_document ran its real handler over each seeded version,
// in order, and its result cites that version.
func plteCheckReads(t *testing.T, w plteWorld, run agenttest.FeatureRun) {
	t.Helper()
	n := 0
	for i, name := range run.Tools {
		if name != agent.ToolReadDocument {
			continue
		}
		if n >= len(w.docs) || !strings.Contains(run.ToolResults[i].Text, w.docs[n].version) {
			t.Fatalf("read_document result %d = %s, want a seeded version", n, run.ToolResults[i].Text)
		}
		n++
	}
	if n != len(w.docs) {
		t.Fatalf("read_document calls = %d, want %d", n, len(w.docs))
	}
}

// plteDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func plteDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: plteDecideAt.Add(-time.Minute),
	}, by)
}

// plteStatus is the HTTP status the API returns for a gate decision error.
func plteStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// plteCheckGate fails t unless the run opened exactly one gate, pending, of kind, prepared
// for the acting user, that neither an agent principal nor the acting user can decide and
// another lawyer can.
func plteCheckGate(t *testing.T, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, kind string) {
	t.Helper()
	ctx := context.Background()
	if len(run.GateIDs) != 1 {
		t.Fatalf("gates = %v, want one", run.GateIDs)
	}
	id := run.GateIDs[0]
	g, err := fd.Seed.Gates.Get(ctx, gate.GateID(id))
	if err != nil {
		t.Fatal(err)
	}
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
		// rule: ARC-I-038
		{"agent principal", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-" + id, ActingFor: agenttest.FeatureActingFor}, gate.ErrAgentApprover, http.StatusForbidden},
		// rule: ARC-I-039
		{"acting user decides own gate", registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, gate.ErrSelfApproval, http.StatusForbidden},
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: plteOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := plteDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || plteStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, plteStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// plteCheckGateKindRefused fails t unless submit_for_approval was refused with
// ErrGateKindNotAllowed, no gate opened and the one draft stays stored.
func plteCheckGateKindRefused(t *testing.T, w plteWorld, run agenttest.FeatureRun) {
	t.Helper()
	if len(run.Refused) != 1 || run.Refused[0].Tool != agent.ToolSubmitForApproval || !errors.Is(run.Refused[0].Err, agenttools.ErrGateKindNotAllowed) {
		t.Fatalf("refused = %v, want submit_for_approval refused with ErrGateKindNotAllowed", run.Refused)
	}
	if len(run.GateIDs) != 0 || len(w.fd.Seed.Store.Drafts) != 1 {
		t.Fatalf("gates opened: %v, drafts %d; want none and the one draft", run.GateIDs, len(w.fd.Seed.Store.Drafts))
	}
}

// plteCheckInjection fails t unless run made exactly calls, a read_document result carried
// injected, the text reached the model only as quoted data and is absent from every write
// input and gate payload.
func plteCheckInjection(t *testing.T, w plteWorld, run agenttest.FeatureRun, calls []plteCall, injected string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	if want := plteToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, want)
	}
	seen := false
	for i, name := range run.Tools {
		if name == agent.ToolReadDocument && strings.Contains(run.ToolResults[i].Text, injected) {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("no read_document result carries the seeded text")
	}
	// rule: ARC-I-038
	if err := agenttest.QuotedOnly(run.Run, injected); err != nil {
		t.Fatal(err)
	}
	if err := plteAbsent(context.Background(), w.fd, run, injected); err != nil {
		t.Fatal(err)
	}
}

// plteAbsent reports an error when text is in any write input of run or in the payload of
// any gate the run opened.
func plteAbsent(ctx context.Context, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, text string) error {
	writes := map[string]bool{}
	for _, s := range fd.Feature.Tools {
		writes[s.Name] = s.Kind == agent.Write
	}
	for _, req := range run.Requests {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Type == "tool_use" && writes[b.ToolName] && strings.Contains(string(b.Input), text) {
					return fmt.Errorf("write input carries the injected text: %s", b.Input)
				}
			}
		}
	}
	for _, id := range run.GateIDs {
		g, err := fd.Seed.Gates.Get(ctx, gate.GateID(id))
		if err != nil {
			return err
		}
		if strings.Contains(string(g.Payload), text) {
			return fmt.Errorf("gate %s payload carries the injected text", id)
		}
	}
	return nil
}

// plteDate is an ISO date in a draft body.
var plteDate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)

// plteSourcedDates checks every date in text against the values run's tool results returned,
// with the production provenance check: a date no read returned is ErrUnsourcedValue.
func plteSourcedDates(runID string, run agenttest.FeatureRun, text string) error {
	pv := agenttools.NewProvenance()
	for _, b := range run.ToolResults {
		if !b.IsError {
			pv.Observe(runID, json.RawMessage(b.Text))
		}
	}
	for _, d := range plteDate.FindAllString(text, -1) {
		in, err := json.Marshal(map[string]string{"date": d})
		if err != nil {
			return err
		}
		if err := pv.Check(runID, []string{"/date"}, in); err != nil {
			return err
		}
	}
	return nil
}

// plteFeatureSpec is one spec/ai/features.json feature with its trigger and screen actions.
type plteFeatureSpec struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Source    string   `json:"source"`
	Trigger   string   `json:"trigger"`
	OnRequest []string `json:"on_request"`
	Tools     []string `json:"tools"`
	ToolGap   []string `json:"tool_gap"`
	ModelCall string   `json:"model_call"`
	Fixture   string   `json:"fixture"`
	Gate      struct {
		Kind []string `json:"kind"`
	} `json:"gate"`
}

// plteCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry and a
// feature Set takes it once.
func plteCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	var spec struct {
		Features []plteFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f plteFeatureSpec) bool { return f.ID == d.ID })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", d.ID)
	}
	sf := spec.Features[i]
	sorted := func(s []string) string { s = slices.Clone(s); slices.Sort(s); return strings.Join(s, ",") }
	kind := d.Kind
	if kind == "" {
		kind = ai.KindToolUse
	}
	tests := []struct {
		field     string
		got, want string
	}{
		{"area", d.Area, "platform"},
		{"name", d.Name, sf.Name},
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
	// rule: ARC-I-035
	// A tool-gap kind is one of the Def's gate kinds, opened by draft_document then
	// submit_for_approval.
	for _, k := range sf.ToolGap {
		if !slices.Contains(d.GateKinds, k) || !slices.Contains(d.Tools, agent.ToolSubmitForApproval) {
			t.Fatalf("tool-gap kind %s: gate kinds %v tools %v", k, d.GateKinds, d.Tools)
		}
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

// plteCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble,
// sits under the shared rules and holds every phrase in want.
func plteCheckPrompt(t *testing.T, d feature.Def, want ...string) {
	t.Helper()
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
	if !strings.HasPrefix(spec.System, feature.Preamble(d.ID)) {
		t.Fatalf("system prompt does not start with the feature preamble")
	}
	for _, w := range append([]string{feature.Rules()}, want...) {
		if !strings.Contains(spec.System, w) {
			t.Errorf("system prompt lacks %q", w)
		}
	}
}

// plteCheckBinding fails t unless b binds d to events and cadence, acting for actingFor, and
// d runs on request from exactly actions.
func plteCheckBinding(t *testing.T, b trigger.Binding, err error, d feature.Def, events []string, cadence trigger.Cadence, actingFor string, actions ...string) {
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
		{"acting for", b.ActingFor, actingFor},
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

// plteCheckDesignated fails t unless b is a scheduled binding for the designated lawyer, never
// a fixed user, so that with no designated lawyer named the feature is unready.
func plteCheckDesignated(t *testing.T, b trigger.Binding) {
	t.Helper()
	if !b.Designated || b.ActingFor != "" {
		t.Fatalf("binding = %+v, want designated with no fixed acting user", b)
	}
	s := trigger.New(nil, nil, nil)
	if err := s.Add(b); err != nil {
		t.Fatalf("Add: %v", err)
	}
	unready, err := s.Readiness(context.Background())
	want := trigger.Unready{FeatureID: b.FeatureID, Reason: trigger.ReasonNoDesignatedLawyer}
	if err != nil || len(unready) != 1 || unready[0] != want {
		t.Fatalf("readiness = %+v, %v; want %+v", unready, err, want)
	}
}
