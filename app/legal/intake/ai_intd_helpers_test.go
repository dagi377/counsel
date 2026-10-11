package intake_test

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
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// Shared test helpers of the intake drafting features (F-INT-10, F-INT-12, F-INT-14). Every
// name carries the intd prefix.
//
// The end-to-end tests drive a Def with agenttest.FromFeature over the app's real handlers and
// no stand-ins: the caller's matter and contact are seeded with the agenttest Seed, so
// search_contacts and search_matters read real records, draft_document stores its draft in the
// Seed's store and submit_for_approval resolves that draft to its matter through the Seed.

// intdOtherLawyer is a second synthetic user who holds every gate kind's approver capability.
const intdOtherLawyer = "user-synthetic-intake-reviewer"

// intdDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var intdDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// intdDraftID is the id the Seed gives the run's first draft: the world seeds no document, so
// the draft is the first record of the store's sequence.
const intdDraftID = "draft-document-1"

// intdWorld is one seeded run: the driver and the matter the scripts draft on.
type intdWorld struct {
	fd       *agenttest.FeatureDriver
	matterID string
}

// intdFeature builds def's FeatureDriver over the real handlers with a gate store in which the
// acting user and intdOtherLawyer hold every kind's approver capability, so the preparer rule,
// not a missing capability, is what stops the acting user. It seeds one matter, short name
// shortName, whose client contact is "<shortName> Client Inc.".
func intdFeature(t *testing.T, def feature.Def, shortName string) intdWorld {
	t.Helper()
	fd := agenttest.FromFeature(t, def)
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, intdOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return intdDecideAt }, caps)
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	m := fd.Seed.Matter("2026-0410", shortName)
	return intdWorld{fd: fd, matterID: string(m.ID)}
}

// intdCall is one scripted model tool call.
type intdCall struct {
	name  string
	input string // JSON
}

// intdCalls is the script every intake drafting feature runs on w: look the caller up by
// query, plan, draft title and body on w's matter, then submit the draft under gateKind.
func intdCalls(w intdWorld, query, title, body, gateKind string) []intdCall {
	return []intdCall{
		{agent.ToolSearchContacts, `{"query": "` + query + `", "limit": 5}`},
		{agent.ToolSearchMatters, `{"query": "` + query + `"}`},
		{agenttools.ToolReportPlan, `{"steps": [{"step_id": "find", "title": "Look up the contact and matter"}, {"step_id": "draft", "title": "Draft and send for approval"}]}`},
		{agent.ToolDraftDocument, `{"matter_id": "` + w.matterID + `", "title": "` + title + `", "body_markdown": "` + body + `"}`},
		{agent.ToolSubmitForApproval, `{"subject_type": "document", "subject_id": "` + intdDraftID + `", "gate_kind": "` + gateKind + `"}`},
	}
}

// intdCiting returns calls with a citation of a version no read of the run returned added to
// the draft_document call.
func intdCiting(calls []intdCall) []intdCall {
	out := slices.Clone(calls)
	for i := range out {
		if out[i].name == agent.ToolDraftDocument {
			out[i].input = strings.TrimSuffix(out[i].input, "}") + `, "citations": [{"anchor": "1", "citation": {"doc_id": "00000000-0000-4000-8000-0000000000d1", ` +
				`"version_id": "00000000-0000-4000-8000-0000000000d2", "page": 1, "line_start": 1, "line_end": 1, "quote_hash": "sha256:` + strings.Repeat("0", 64) + `"}}]}`
		}
	}
	return out
}

// intdScript is an inline aifake fixture for featureID under dir: one tool call per turn, then
// text.
func intdScript(featureID, dir string, calls []intdCall, text string) *aifake.Fake {
	var b strings.Builder
	fmt.Fprintf(&b, "synthetic: true\nfeature: %s\nmatch:\n  last_user_regex: '.'\nturns:\n", featureID)
	for _, c := range calls {
		fmt.Fprintf(&b, "  - reply:\n      tool_calls:\n        - name: %s\n          input: %s\n      stop_reason: tool_use\n", c.name, c.input)
	}
	fmt.Fprintf(&b, "  - reply:\n      text: %q\n      stop_reason: end_turn\n", text)
	return aifake.New(fstest.MapFS{strings.ToLower(featureID) + "/" + dir + "/ok.yaml": {Data: []byte(b.String())}})
}

// intdRun fires w's feature once with calls scripted under dir.
func intdRun(w intdWorld, dir string, calls []intdCall, prompt string) agenttest.FeatureRun {
	model := intdScript(w.fd.Def.ID, dir, calls, "Drafted for approval.")
	return w.fd.Fire(context.Background(), model, "run-"+strings.ToLower(w.fd.Def.ID), prompt)
}

// intdToolNames lists the tool names of calls.
func intdToolNames(calls []intdCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// intdCheckRun fails t unless run used no stand-in, ran every call, planned before its first
// write (draft_document), stored one draft prepared for the acting user on w's matter and
// opened one gate of kind that only another lawyer can decide.
func intdCheckRun(t *testing.T, w intdWorld, run agenttest.FeatureRun, calls []intdCall, kind string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	// rule: ARC-I-035
	if len(w.fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want every tool on its real handler", w.fd.StandIns, run.StandIns)
	}
	if want := intdToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	// rule: ARC-I-039
	if run.PlanAt < 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != agent.ToolDraftDocument {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before draft_document", run.PlanAt, run.FirstWrite, run.Tools)
	}
	// search_contacts ran its real handler over the seeded matter's client.
	if !strings.Contains(run.ToolResults[0].Text, w.matterID) {
		t.Fatalf("search_contacts result = %s, want the seeded matter %s", run.ToolResults[0].Text, w.matterID)
	}
	// The draft is stored for the acting user, never in the agent's name.
	drafts := w.fd.Seed.Store.Drafts
	if len(drafts) != 1 || drafts[0].ID != intdDraftID || drafts[0].PreparedBy != agenttest.FeatureActingFor || drafts[0].MatterID != w.matterID {
		t.Fatalf("drafts = %+v, want one %s on %s prepared by %s", drafts, intdDraftID, w.matterID, agenttest.FeatureActingFor)
	}
	intdCheckGate(t, w.fd, run, kind)
}

// intdDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func intdDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: intdDecideAt.Add(-time.Minute),
	}, by)
}

// intdStatus is the HTTP status the API returns for a gate decision error.
func intdStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// intdCheckGate fails t unless the run opened exactly one gate, pending, of kind, prepared for
// the acting user, that neither an agent principal nor the acting user can decide and another
// lawyer can.
func intdCheckGate(t *testing.T, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, kind string) {
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
	// rule: AI-R-003
	if g.Kind != kind || g.Status != gate.StatusPending || g.Preparer.ActingFor != agenttest.FeatureActingFor {
		t.Fatalf("gate = %s %s preparer %+v; want pending %s prepared for %s", g.Kind, g.Status, g.Preparer, kind, agenttest.FeatureActingFor)
	}
	tests := []struct {
		name       string
		by         registry.Principal
		wantErr    error
		wantStatus int
	}{
		// rule: AI-R-003
		{"agent principal", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-" + id, ActingFor: agenttest.FeatureActingFor}, gate.ErrAgentApprover, http.StatusForbidden},
		{"acting user decides own gate", registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, gate.ErrSelfApproval, http.StatusForbidden},
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: intdOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := intdDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || intdStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, intdStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// intdCheckGateKindRefused fails t unless submit_for_approval was refused with
// ErrGateKindNotAllowed, no gate opened and the one draft stays stored.
func intdCheckGateKindRefused(t *testing.T, w intdWorld, run agenttest.FeatureRun) {
	t.Helper()
	if len(run.Refused) != 1 || run.Refused[0].Tool != agent.ToolSubmitForApproval || !errors.Is(run.Refused[0].Err, agenttools.ErrGateKindNotAllowed) {
		t.Fatalf("refused = %v, want submit_for_approval refused with ErrGateKindNotAllowed", run.Refused)
	}
	if len(run.GateIDs) != 0 || len(w.fd.Seed.Store.Drafts) != 1 {
		t.Fatalf("gates opened: %v, drafts %d; want none and the one draft", run.GateIDs, len(w.fd.Seed.Store.Drafts))
	}
}

// intdCheckUncitedRefused fails t unless draft_document was refused with ErrUncitedSource for
// a citation the run never read, so no draft is stored and no gate opens.
func intdCheckUncitedRefused(t *testing.T, w intdWorld, run agenttest.FeatureRun) {
	t.Helper()
	// rule: ARC-I-038
	if len(run.Refused) == 0 || run.Refused[0].Tool != agent.ToolDraftDocument || !errors.Is(run.Refused[0].Err, agent.ErrUncitedSource) {
		t.Fatalf("refused = %v, want draft_document refused with ErrUncitedSource", run.Refused)
	}
	if len(run.GateIDs) != 0 || len(w.fd.Seed.Store.Drafts) != 0 {
		t.Fatalf("gates %v, drafts %d; want none", run.GateIDs, len(w.fd.Seed.Store.Drafts))
	}
}

// intdCheckInjection fails t unless run made exactly calls, the injected text reached the
// model only as quoted data and is absent from every write input and gate payload.
func intdCheckInjection(t *testing.T, w intdWorld, run agenttest.FeatureRun, calls []intdCall, injected string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	if want := intdToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, want)
	}
	// rule: ARC-I-038
	if err := agenttest.QuotedOnly(run.Run, injected); err != nil {
		t.Fatal(err)
	}
	if err := intdAbsent(context.Background(), w.fd, run, injected); err != nil {
		t.Fatal(err)
	}
}

// intdAbsent reports an error when text is in any write input of run or in the payload of any
// gate the run opened.
func intdAbsent(ctx context.Context, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, text string) error {
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

// intdFeatureSpec is one spec/ai/features.json feature.
type intdFeatureSpec struct {
	ID        string   `json:"id"`
	Area      string   `json:"area"`
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

// intdSpec returns id's spec/ai/features.json entry.
func intdSpec(t *testing.T, id string) intdFeatureSpec {
	t.Helper()
	var spec struct {
		Features []intdFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f intdFeatureSpec) bool { return f.ID == id })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", id)
	}
	return spec.Features[i]
}

// intdCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry (the spec
// sync check for this id), a feature Set takes it once and its run scope is its gate kinds.
// The spec words a screen action's button in its trigger and leaves on_request empty
// (Q-FEAT-CON-10), so a screen action's Actions are checked against its trigger.
func intdCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	sf := intdSpec(t, d.ID)
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
		{"area", d.Area, sf.Area},
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

// intdCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble and
// the shared rules, and names the task and the expected output from features.json plus every
// phrase in want.
func intdCheckPrompt(t *testing.T, d feature.Def, want ...string) {
	t.Helper()
	sf := intdSpec(t, d.ID)
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
	for _, w := range append([]string{feature.Rules(), sf.Name, sf.Outputs, "report_plan", "draft_document"}, want...) {
		if !strings.Contains(spec.System, w) {
			t.Errorf("system prompt lacks %q", w)
		}
	}
}
