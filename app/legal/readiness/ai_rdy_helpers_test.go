package readiness_test

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

// The rdy helpers drive the readiness features with agenttest.FromFeatureWith over the app's
// production handler set, agent.Handlers(), wired as boot.AgentTools wires it: every read
// tool records its dates and numbers into the run's provenance. A run searches one seeded
// matter and the documents filed on it with Seed.Document, and writes on that matter: the
// matter the trigger names, never a made-up one.

// rdyOtherLawyer is a second synthetic user who holds every gate kind's approver capability.
const rdyOtherLawyer = "user-synthetic-rdy-lawyer"

// rdyDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var rdyDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// rdyWorld is one seeded run: the driver, the matter the run searches and the versions of
// the documents filed on it.
type rdyWorld struct {
	fd       *agenttest.FeatureDriver
	matterID string
	versions []string
}

// rdyHandlers is agent.Handlers() with every read tool recording into the run's provenance
// (the *agenttools.Provenance in the Deps the Seed passes), as boot.AgentTools does, so a
// sourced write may carry a date a search returned.
func rdyHandlers(t *testing.T) agent.HandlerSet {
	t.Helper()
	app := agent.Handlers()
	var hs []agent.Handler
	for _, name := range app.Names() {
		h, _ := app.For(name)
		build := h.Build
		hs = append(hs, agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
			tool, err := build(d)
			if err != nil || tool.Kind != registry.ToolRead {
				return tool, err
			}
			pv, err := agent.DepOf[*agenttools.Provenance](d)
			if err != nil {
				return registry.Tool{}, err
			}
			// rule: ARC-U-027
			return pv.ReadTool(tool)
		}})
	}
	set, err := agent.NewHandlerSet(hs...)
	if err != nil {
		t.Fatalf("handler set: %v", err)
	}
	return set
}

// rdyFeature builds def's FeatureDriver over the production handlers with a gate store in
// which the acting user and rdyOtherLawyer hold every kind's approver capability, so the
// preparer rule, not a missing capability, is what stops the acting user. It seeds matter
// number with shortName and files one single-page document per entry of docs on it.
func rdyFeature(t *testing.T, def feature.Def, number, shortName string, docs ...[]string) rdyWorld {
	t.Helper()
	fd := agenttest.FromFeatureWith(t, def, rdyHandlers(t))
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, rdyOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return rdyDecideAt }, caps)
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	m := fd.Seed.Matter(number, shortName)
	w := rdyWorld{fd: fd, matterID: string(m.ID)}
	for _, lines := range docs {
		_, v := fd.Seed.Document(w.matterID, lines)
		w.versions = append(w.versions, string(v))
	}
	return w
}

// draftID is the id the Seed gives the run's first draft document: its store numbers
// documents and drafts in one sequence, after the seeded documents.
func (w rdyWorld) draftID() string {
	return fmt.Sprintf("draft-document-%d", len(w.versions)+1)
}

// rdyCall is one scripted model tool call.
type rdyCall struct {
	name  string
	input string // JSON
}

// rdyJSON marshals v for a script, failing t on error.
func rdyJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// rdySearch is one search_documents call on w's matter.
func rdySearch(t *testing.T, w rdyWorld, query string) rdyCall {
	t.Helper()
	return rdyCall{agent.ToolSearchDocuments, rdyJSON(t, map[string]string{"matter_id": w.matterID, "query": query})}
}

// rdyPlan is the report_plan call every script makes before its first write.
const rdyPlan = `{"steps": [{"step_id": "search", "title": "Search the matter's records"}, {"step_id": "propose", "title": "Draft and send for approval"}]}`

// rdyDocWrites plans, drafts body on w's matter with draft_document and submits the draft
// under gateKind.
func rdyDocWrites(t *testing.T, w rdyWorld, title, body, gateKind string) []rdyCall {
	t.Helper()
	return []rdyCall{
		{agent.ToolReportPlan, rdyPlan},
		{agent.ToolDraftDocument, rdyJSON(t, map[string]string{"matter_id": w.matterID, "title": title, "body_markdown": body})},
		{agent.ToolSubmitForApproval, rdyJSON(t, map[string]string{"subject_type": "document", "subject_id": w.draftID(), "gate_kind": gateKind})},
	}
}

// rdyScript is an inline aifake fixture for feature: one tool call per turn, then text.
func rdyScript(feature, dir string, calls []rdyCall, text string) *aifake.Fake {
	var b strings.Builder
	fmt.Fprintf(&b, "synthetic: true\nfeature: %s\nmatch:\n  last_user_regex: '.'\nturns:\n", feature)
	for _, c := range calls {
		fmt.Fprintf(&b, "  - reply:\n      tool_calls:\n        - name: %s\n          input: %s\n      stop_reason: tool_use\n", c.name, c.input)
	}
	fmt.Fprintf(&b, "  - reply:\n      text: %q\n      stop_reason: end_turn\n", text)
	return aifake.New(fstest.MapFS{strings.ToLower(feature) + "/" + dir + "/ok.yaml": {Data: []byte(b.String())}})
}

// rdyToolNames lists the tool names of calls.
func rdyToolNames(calls []rdyCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// rdyCheckRun fails t unless run used no stand-in, ran every call, planned before its first
// write (firstWrite), searched the seeded documents for real and opened one gate of kind that
// only another lawyer can decide. A draft_document run must also store one draft on w's
// matter prepared for the acting user.
func rdyCheckRun(t *testing.T, w rdyWorld, run agenttest.FeatureRun, calls []rdyCall, firstWrite, kind string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	// rule: ARC-I-035
	if len(w.fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want every tool on its production handler", w.fd.StandIns, run.StandIns)
	}
	if want := rdyToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	// rule: ARC-I-039
	if run.PlanAt < 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != firstWrite {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before %s", run.PlanAt, run.FirstWrite, run.Tools, firstWrite)
	}
	rdyCheckSearches(t, w, run)
	if firstWrite == agent.ToolDraftDocument {
		// The draft is stored for the acting user, never in the agent's name, on the matter
		// the run searched.
		drafts := w.fd.Seed.Store.Drafts
		if len(drafts) != 1 || drafts[0].ID != w.draftID() || drafts[0].PreparedBy != agenttest.FeatureActingFor || drafts[0].MatterID != w.matterID {
			t.Fatalf("drafts = %+v, want one draft on %s prepared by %s", drafts, w.matterID, agenttest.FeatureActingFor)
		}
	}
	rdyCheckGate(t, w.fd, run, kind)
}

// rdyCheckSearches fails t unless every search_documents call ran its production handler and
// returned quoted lines of the seeded versions only.
func rdyCheckSearches(t *testing.T, w rdyWorld, run agenttest.FeatureRun) {
	t.Helper()
	n := 0
	for i, name := range run.Tools {
		if name != agent.ToolSearchDocuments {
			continue
		}
		n++
		var out struct {
			Quotes []agenttools.Quoted `json:"quotes"`
		}
		if err := json.Unmarshal([]byte(run.ToolResults[i].Text), &out); err != nil || len(out.Quotes) == 0 {
			t.Fatalf("search_documents result %s (%v), want quoted hits", run.ToolResults[i].Text, err)
		}
		for _, q := range out.Quotes {
			if !q.Untrusted || !slices.Contains(w.versions, q.Source.VersionID) {
				t.Fatalf("search hit %+v, want an untrusted quote of a seeded version", q)
			}
		}
	}
	if n == 0 {
		t.Fatalf("no search_documents call in %v", run.Tools)
	}
}

// rdyDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func rdyDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: rdyDecideAt.Add(-time.Minute),
	}, by)
}

// rdyStatus is the HTTP status the API returns for a gate decision error.
func rdyStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// rdyCheckGate fails t unless the run opened exactly one gate, pending, of kind, prepared for
// the acting user, that neither an agent principal nor the acting user can decide and another
// lawyer can.
func rdyCheckGate(t *testing.T, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, kind string) {
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
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: rdyOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rdyDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || rdyStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, rdyStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// rdyCheckGateKindRefused fails t unless submit_for_approval was refused with
// ErrGateKindNotAllowed, no gate opened and the one draft stays stored.
func rdyCheckGateKindRefused(t *testing.T, w rdyWorld, run agenttest.FeatureRun) {
	t.Helper()
	if len(run.Refused) != 1 || run.Refused[0].Tool != agent.ToolSubmitForApproval || !errors.Is(run.Refused[0].Err, agenttools.ErrGateKindNotAllowed) {
		t.Fatalf("refused = %v, want submit_for_approval refused with ErrGateKindNotAllowed", run.Refused)
	}
	if len(run.GateIDs) != 0 || len(w.fd.Seed.Store.Drafts) != 1 {
		t.Fatalf("gates opened: %v, drafts %d; want none and the one draft", run.GateIDs, len(w.fd.Seed.Store.Drafts))
	}
}

// rdyCheckInjection fails t unless run made exactly calls, a search_documents result carried
// injected, the text reached the model only as quoted data and is absent from every write
// input and gate payload.
func rdyCheckInjection(t *testing.T, w rdyWorld, run agenttest.FeatureRun, calls []rdyCall, injected string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	if want := rdyToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, want)
	}
	seen := false
	for i, name := range run.Tools {
		if name == agent.ToolSearchDocuments && strings.Contains(run.ToolResults[i].Text, injected) {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("no search_documents result carries the seeded text")
	}
	// rule: ARC-I-038
	if err := agenttest.QuotedOnly(run.Run, injected); err != nil {
		t.Fatal(err)
	}
	if err := rdyAbsent(context.Background(), w.fd, run, injected); err != nil {
		t.Fatal(err)
	}
}

// rdyAbsent reports an error when text is in any write input of run or in the payload of any
// gate the run opened.
func rdyAbsent(ctx context.Context, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, text string) error {
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

// Dates and amounts in a draft body: an ISO date, and a dollar amount with optional thousands
// commas and cents.
var (
	rdyDate   = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	rdyAmount = regexp.MustCompile(`\$(\d{1,3}(?:,\d{3})*(?:\.\d{2})?)`)
)

// rdySourced checks every date and dollar amount in text against the values run's tool
// results returned, with the production provenance check: a value no read returned is
// ErrUnsourcedValue.
func rdySourced(runID string, run agenttest.FeatureRun, text string) error {
	pv := agenttools.NewProvenance()
	for _, b := range run.ToolResults {
		if !b.IsError {
			pv.Observe(runID, json.RawMessage(b.Text))
		}
	}
	var ins []string
	for _, d := range rdyDate.FindAllString(text, -1) {
		ins = append(ins, fmt.Sprintf(`{"v": %q}`, d))
	}
	for _, m := range rdyAmount.FindAllStringSubmatch(text, -1) {
		ins = append(ins, `{"v": `+strings.ReplaceAll(m[1], ",", "")+`}`)
	}
	for _, in := range ins {
		if err := pv.Check(runID, []string{"/v"}, json.RawMessage(in)); err != nil {
			return err
		}
	}
	return nil
}

// rdyFeatureSpec is one spec/ai/features.json feature with its trigger and screen actions.
type rdyFeatureSpec struct {
	ID        string   `json:"id"`
	Area      string   `json:"area"`
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

// rdyCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry and a
// feature Set takes it once.
func rdyCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	var spec struct {
		Features []rdyFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f rdyFeatureSpec) bool { return f.ID == d.ID })
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
		{"area", d.Area, sf.Area},
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

// rdyCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble, sits
// under the shared rules and holds every phrase in want.
func rdyCheckPrompt(t *testing.T, d feature.Def, want ...string) {
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

// rdyCheckBinding fails t unless b binds d to events with no schedule, acting for the event's
// user, and d runs on request from exactly actions.
func rdyCheckBinding(t *testing.T, b trigger.Binding, err error, d feature.Def, events []string, actions ...string) {
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
		{"no schedule", b.Cadence, trigger.CadenceNone},
		{"acts for the event user", b.ActingFor, ""},
		{"not designated", b.Designated || b.PerUser, false},
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
