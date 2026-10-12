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
	"github.com/dagi377/counsel/app/legal/agent/schemas"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// The platform features' end-to-end tests drive a Def with agenttest.FromFeature over the
// app's real handlers, with no stand-ins. A run reads one seeded matter and the documents
// filed on it with Seed.Document; a firm-level draft (subject_ref "firm") names no matter.

// pltdOtherLawyer is a second synthetic user who holds every gate kind's approver capability.
const pltdOtherLawyer = "user-synthetic-pltd-lawyer"

// pltdDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var pltdDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// pltdDoc is one seeded document: its id and the version id read_document must be given.
type pltdDoc struct {
	id      string
	version string
}

// pltdWorld is one seeded run: the driver, the matter the run reads and its documents.
type pltdWorld struct {
	fd       *agenttest.FeatureDriver
	matterID string
	clientID string
	docs     []pltdDoc
}

// pltdFeature builds def's FeatureDriver over the real handlers with a gate store in which
// the acting user and pltdOtherLawyer hold every kind's approver capability, so the preparer
// rule, not a missing capability, is what stops the acting user. It seeds matter number with
// shortName and files one single-page document per entry of docs on it.
func pltdFeature(t *testing.T, def feature.Def, number, shortName string, docs ...[]string) pltdWorld {
	t.Helper()
	fd := agenttest.FromFeature(t, def)
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, pltdOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return pltdDecideAt }, caps)
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	m := fd.Seed.Matter(number, shortName)
	w := pltdWorld{fd: fd, matterID: string(m.ID), clientID: string(m.ClientID)}
	for _, lines := range docs {
		id, v := fd.Seed.Document(w.matterID, lines)
		w.docs = append(w.docs, pltdDoc{id: id, version: string(v)})
	}
	return w
}

// draftID is the id the Seed gives the run's first draft of kind ("document" or "email"):
// its store numbers documents and drafts in one sequence, after the seeded documents.
func (w pltdWorld) draftID(kind string) string {
	return fmt.Sprintf("draft-%s-%d", kind, len(w.docs)+1)
}

// pltdCall is one scripted model tool call.
type pltdCall struct {
	name  string
	input string // JSON
}

// pltdJSON marshals v for a script, failing t on error.
func pltdJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// pltdReads are the read calls a script makes on w: the matter, one search of its documents
// and a read of every seeded document at its version.
func pltdReads(t *testing.T, w pltdWorld, query string) []pltdCall {
	t.Helper()
	calls := []pltdCall{
		{agent.ToolGetMatter, pltdJSON(t, map[string]string{"matter_id": w.matterID})},
		{agent.ToolSearchDocuments, pltdJSON(t, map[string]string{"matter_id": w.matterID, "query": query})},
	}
	for _, d := range w.docs {
		calls = append(calls, pltdCall{agent.ToolReadDocument, pltdJSON(t, map[string]string{"doc_id": d.id, "version_id": d.version})})
	}
	return calls
}

// pltdFirmWrites plans, drafts body on the firm (subject_ref "firm", no matter_id) and
// submits the draft under gateKind.
func pltdFirmWrites(t *testing.T, w pltdWorld, title, body, gateKind string) []pltdCall {
	t.Helper()
	return []pltdCall{
		{agenttools.ToolReportPlan, `{"steps": [{"step_id": "read", "title": "Read the records"}, {"step_id": "draft", "title": "Draft and send for approval"}]}`},
		{agent.ToolDraftDocument, pltdJSON(t, map[string]string{"subject_ref": agent.SubjectFirm, "title": title, "body_markdown": body})},
		{agent.ToolSubmitForApproval, pltdJSON(t, map[string]string{"subject_type": "document", "subject_id": w.draftID("document"), "gate_kind": gateKind})},
	}
}

// pltdScript is an inline aifake fixture for feature: one tool call per turn, then text.
func pltdScript(feature, dir string, calls []pltdCall, text string) *aifake.Fake {
	var b strings.Builder
	fmt.Fprintf(&b, "synthetic: true\nfeature: %s\nmatch:\n  last_user_regex: '.'\nturns:\n", feature)
	for _, c := range calls {
		fmt.Fprintf(&b, "  - reply:\n      tool_calls:\n        - name: %s\n          input: %s\n      stop_reason: tool_use\n", c.name, c.input)
	}
	fmt.Fprintf(&b, "  - reply:\n      text: %q\n      stop_reason: end_turn\n", text)
	return aifake.New(fstest.MapFS{strings.ToLower(feature) + "/" + dir + "/ok.yaml": {Data: []byte(b.String())}})
}

// pltdToolNames lists the tool names of calls.
func pltdToolNames(calls []pltdCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// pltdCheckRun fails t unless run used no stand-in, ran every call, planned before its first
// write (draft_document), read every seeded document for real at its version, stored one
// firm draft with no matter prepared for the acting user and opened one gate of kind that
// only another lawyer can decide.
func pltdCheckRun(t *testing.T, w pltdWorld, run agenttest.FeatureRun, calls []pltdCall, kind string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	// rule: ARC-I-035
	if len(w.fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want every tool on its real handler", w.fd.StandIns, run.StandIns)
	}
	if want := pltdToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	// rule: ARC-I-039
	if run.PlanAt < 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != agent.ToolDraftDocument {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before draft_document", run.PlanAt, run.FirstWrite, run.Tools)
	}
	pltdCheckReads(t, w, run)
	// The draft is the firm's, stored for the acting user, never in the agent's name, and it
	// borrows no matter, not even the one the run read.
	drafts := w.fd.Seed.Store.Drafts
	if len(drafts) != 1 || drafts[0].ID != w.draftID("document") || drafts[0].PreparedBy != agenttest.FeatureActingFor || drafts[0].MatterID != "" {
		t.Fatalf("drafts = %+v, want one firm draft %s with no matter prepared by %s", drafts, w.draftID("document"), agenttest.FeatureActingFor)
	}
	pltdCheckGate(t, w.fd, run, kind)
}

// pltdCheckReads fails t unless read_document ran its real handler over each seeded version,
// in order, and its result cites that version.
func pltdCheckReads(t *testing.T, w pltdWorld, run agenttest.FeatureRun) {
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

// pltdDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func pltdDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: pltdDecideAt.Add(-time.Minute),
	}, by)
}

// pltdStatus is the HTTP status the API returns for a gate decision error.
func pltdStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// pltdCheckGate fails t unless the run opened exactly one gate, pending, of kind, prepared
// for the acting user, that neither an agent principal nor the acting user can decide and
// another lawyer can.
func pltdCheckGate(t *testing.T, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, kind string) {
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
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: pltdOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pltdDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || pltdStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, pltdStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// pltdCheckGateKindRefused fails t unless submit_for_approval was refused with
// ErrGateKindNotAllowed, no gate opened and the one draft stays stored.
func pltdCheckGateKindRefused(t *testing.T, w pltdWorld, run agenttest.FeatureRun) {
	t.Helper()
	if len(run.Refused) != 1 || run.Refused[0].Tool != agent.ToolSubmitForApproval || !errors.Is(run.Refused[0].Err, agenttools.ErrGateKindNotAllowed) {
		t.Fatalf("refused = %v, want submit_for_approval refused with ErrGateKindNotAllowed", run.Refused)
	}
	if len(run.GateIDs) != 0 || len(w.fd.Seed.Store.Drafts) != 1 {
		t.Fatalf("gates opened: %v, drafts %d; want none and the one draft", run.GateIDs, len(w.fd.Seed.Store.Drafts))
	}
}

// pltdCheckInjection fails t unless run made exactly calls, a read_document result carried
// injected, the text reached the model only as quoted data and is absent from every write
// input and gate payload.
func pltdCheckInjection(t *testing.T, w pltdWorld, run agenttest.FeatureRun, calls []pltdCall, injected string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	if want := pltdToolNames(calls); !slices.Equal(run.Tools, want) {
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
	if err := pltdAbsent(context.Background(), w.fd, run, injected); err != nil {
		t.Fatal(err)
	}
}

// pltdAbsent reports an error when text is in any write input of run or in the payload of
// any gate the run opened.
func pltdAbsent(ctx context.Context, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, text string) error {
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

// pltdDate is an ISO date in a draft body.
var pltdDate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)

// pltdSourcedDates checks every date in text against the values run's tool results returned,
// with the production provenance check: a date no read returned is ErrUnsourcedValue.
func pltdSourcedDates(runID string, run agenttest.FeatureRun, text string) error {
	pv := agenttools.NewProvenance()
	for _, b := range run.ToolResults {
		if !b.IsError {
			pv.Observe(runID, json.RawMessage(b.Text))
		}
	}
	for _, d := range pltdDate.FindAllString(text, -1) {
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

// pltdFeatureSpec is one spec/ai/features.json feature with its trigger and screen actions.
type pltdFeatureSpec struct {
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

// pltdCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry and a
// feature Set takes it once. The spec words a screen action's button in its trigger and leaves
// on_request empty (Q-FEAT-CON-10), so a screen action's Actions are checked against its trigger.
func pltdCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	var spec struct {
		Features []pltdFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f pltdFeatureSpec) bool { return f.ID == d.ID })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", d.ID)
	}
	sf := spec.Features[i]
	onRequest := sf.OnRequest
	if sf.Source == string(feature.SourceScreenAction) && len(onRequest) == 0 {
		onRequest = []string{strings.TrimPrefix(sf.Trigger, "on request: ")}
	}
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
		{"on request", sorted(d.Actions), sorted(onRequest)},
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

// pltdCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble,
// sits under the shared rules and holds every phrase in want.
func pltdCheckPrompt(t *testing.T, d feature.Def, want ...string) {
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

// pltdCheckBinding fails t unless b binds d to events and cadence, acting for actingFor, and
// d runs on request from exactly actions.
func pltdCheckBinding(t *testing.T, b trigger.Binding, err error, d feature.Def, events []string, cadence trigger.Cadence, actingFor string, actions ...string) {
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

// pltdCheckDesignated fails t unless b is a scheduled binding for the designated lawyer, never
// a fixed user, so that with no designated lawyer named the feature is unready.
func pltdCheckDesignated(t *testing.T, b trigger.Binding) {
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

// pltdEmailWrites plans, then drafts body as an email on w's matter to its client and submits
// it in the same call, which opens the outgoing-email gate.
func pltdEmailWrites(t *testing.T, w pltdWorld, subject, body string) []pltdCall {
	t.Helper()
	return []pltdCall{
		{agenttools.ToolReportPlan, `{"steps": [{"step_id": "read", "title": "Read the matter"}, {"step_id": "reply", "title": "Draft the reply for approval"}]}`},
		{agent.ToolDraftEmail, pltdJSON(t, map[string]any{
			"matter_id": w.matterID, "to_contact_ids": []string{w.clientID}, "subject": subject, "body_markdown": body, "submit": true,
		})},
	}
}

// pltdCheckEmailRun fails t unless run used no stand-in, ran every call, planned before its
// first write (draft_email), read every seeded document for real, stored one email on w's
// matter prepared for the acting user under runID and opened one gate of kind that only
// another lawyer can decide.
func pltdCheckEmailRun(t *testing.T, w pltdWorld, run agenttest.FeatureRun, calls []pltdCall, runID, kind string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	// rule: ARC-I-035
	if len(w.fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want every tool on its real handler", w.fd.StandIns, run.StandIns)
	}
	if want := pltdToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	// rule: ARC-I-039
	if run.PlanAt < 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != agent.ToolDraftEmail {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before draft_email", run.PlanAt, run.FirstWrite, run.Tools)
	}
	pltdCheckReads(t, w, run)
	// The reply is stored for the acting user on the matter the text came in on, never in
	// the agent's name.
	ds := w.fd.Seed.Store.Drafts
	if len(ds) != 1 || ds[0].Kind != "email" || ds[0].ID != w.draftID("email") || ds[0].MatterID != w.matterID ||
		ds[0].PreparedBy != agenttest.FeatureActingFor || ds[0].RunID != runID {
		t.Fatalf("drafts = %+v, want one email on %s prepared by %s", ds, w.matterID, agenttest.FeatureActingFor)
	}
	pltdCheckGate(t, w.fd, run, kind)
}

// plthAnswer is a chat run's final answer ({answer, citations}) citing line of page 1 of
// version, the shape run.CheckAnswer reads.
func plthAnswer(t *testing.T, text, version string, line int) string {
	t.Helper()
	return pltdJSON(t, map[string]any{
		"answer":    text,
		"citations": []map[string]any{{"version_id": version, "page": 1, "from_line": line, "to_line": line}},
	})
}

// plthOutcome is the run.Outcome of r for run.CheckAnswer: its final text and every citation
// a tool result of the run carried as quoted data.
func plthOutcome(r agenttest.FeatureRun) run.Outcome {
	out := run.Outcome{Text: r.Text, JSON: r.JSON, GateIDs: r.GateIDs}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if x["untrusted"] == true {
				var q agenttools.Quoted
				if b, err := json.Marshal(x); err == nil && json.Unmarshal(b, &q) == nil && q.Source.Page > 0 {
					out.Citations = append(out.Citations, q.Source)
				}
			}
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	for _, b := range r.ToolResults {
		var v any
		if !b.IsError && json.Unmarshal([]byte(b.Text), &v) == nil {
			walk(v)
		}
	}
	return out
}

// plthCheckReadOnlyDef fails t unless d is a read-only chat feature: every tool is a read,
// and it names no gate kind, so its run scope opens none.
func plthCheckReadOnlyDef(t *testing.T, d feature.Def) {
	t.Helper()
	// rule: AI-R-003
	for _, name := range d.Tools {
		s, err := schemas.ByName(name)
		if err != nil || s.Kind != registry.ToolRead {
			t.Fatalf("tool %s: kind %v (%v), want read", name, s.Kind, err)
		}
	}
	if d.Kind != ai.KindChat || len(d.GateKinds) != 0 {
		t.Fatalf("kind %s gate kinds %v, want a chat with no gate kind", d.Kind, d.GateKinds)
	}
	rs, err := d.Spec()
	if err != nil || len(rs.GateKinds) != 0 {
		t.Fatalf("run gate kinds = %v (%v), want none", rs.GateKinds, err)
	}
}

// plthCheckChatRun fails t unless run used no stand-in, ran exactly calls on their real
// handlers, read every seeded document at its version, wrote nothing and opened no gate, and
// its answer meets run.CheckAnswer with want (nil: every citation came from a tool result of
// this run).
func plthCheckChatRun(t *testing.T, w pltdWorld, d feature.Def, r agenttest.FeatureRun, calls []pltdCall, want error) {
	t.Helper()
	if r.Err != nil || len(r.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", r.Err, r.Refused)
	}
	if len(w.fd.StandIns) != 0 || len(r.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want every tool on its real handler", w.fd.StandIns, r.StandIns)
	}
	if wantTools := pltdToolNames(calls); !slices.Equal(r.Tools, wantTools) {
		t.Fatalf("tools = %v, want %v", r.Tools, wantTools)
	}
	pltdCheckReads(t, w, r)
	// rule: AI-R-003
	// No write ran, nothing was drafted and no gate row was opened.
	if r.FirstWrite >= 0 || len(r.GateIDs) != 0 || len(w.fd.Seed.Store.Drafts) != 0 {
		t.Fatalf("first write %d, gates %v, drafts %d; want none", r.FirstWrite, r.GateIDs, len(w.fd.Seed.Store.Drafts))
	}
	spec, err := d.Spec()
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	// rule: AI-R-005
	// rule: ARC-I-038
	ans, err := run.CheckAnswer(spec, plthOutcome(r))
	if !errors.Is(err, want) {
		t.Fatalf("CheckAnswer = %v, want %v", err, want)
	}
	if err == nil && (len(ans.Citations) == 0 || !slices.ContainsFunc(w.docs, func(doc pltdDoc) bool { return doc.version == ans.Citations[0].VersionID })) {
		t.Fatalf("citations = %v, want a seeded version", ans.Citations)
	}
}
