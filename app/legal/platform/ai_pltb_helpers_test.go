package platform_test

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

// Shared test helpers of the platform features that draft a client reply and open a gate on
// it (F-PLT-01), prefix pltb. Their runs use agenttest.FromFeature over the app's production
// handler set, agent.Handlers(), with no stand-ins. Every record is synthetic.

// pltbOtherLawyer holds every gate kind's approver capability and prepared nothing.
const pltbOtherLawyer = "user-synthetic-portal-reviewer"

// pltbDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var pltbDecideAt = time.Date(2026, 10, 6, 9, 30, 0, 0, time.FixedZone("EDT", -4*60*60))

// pltbFeatureSpec is one spec/ai/features.json feature.
type pltbFeatureSpec struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Area      string   `json:"area"`
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

// pltbSpec returns id's spec/ai/features.json entry.
func pltbSpec(t *testing.T, id string) pltbFeatureSpec {
	t.Helper()
	var spec struct {
		Features []pltbFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f pltbFeatureSpec) bool { return f.ID == id })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", id)
	}
	return spec.Features[i]
}

// pltbSpecSync fails t unless d matches its spec/ai/features.json entry (the spec sync check
// for this id), a feature Set takes it once and its run scope is its gate kinds.
func pltbSpecSync(t *testing.T, d feature.Def) {
	t.Helper()
	sf := pltbSpec(t, d.ID)
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
		{"area", d.Area, sf.Area},
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

// pltbPrompt fails t unless d's prompt passes lint, starts with the feature preamble and the
// shared rules, and names the task, the expected output from features.json and want.
func pltbPrompt(t *testing.T, d feature.Def, want ...string) {
	t.Helper()
	sf := pltbSpec(t, d.ID)
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
	for _, w := range append([]string{sf.Name, sf.Outputs}, want...) {
		if !strings.Contains(spec.System, w) {
			t.Errorf("system prompt lacks %q", w)
		}
	}
}

// pltbWorld is one seeded run: the driver, the matter, its client and its one document.
type pltbWorld struct {
	fd      *agenttest.FeatureDriver
	matter  string
	client  string
	doc     string
	version string
}

// pltbDraftID is the id the Seed gives the run's first email draft: its store numbers
// documents and drafts in one sequence, and the world seeded one document.
const pltbDraftID = "draft-email-2"

// pltbFeature builds def's FeatureDriver over the production handler set with a gate store
// in which the acting user and pltbOtherLawyer hold every kind's approver capability, so the
// preparer rule, not a missing capability, is what stops the acting user. It seeds one matter
// with one document of lines.
func pltbFeature(t *testing.T, def feature.Def, lines []string) pltbWorld {
	t.Helper()
	fd := agenttest.FromFeature(t, def)
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, pltbOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return pltbDecideAt }, caps)
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	m := fd.Seed.Matter("2026-0301", "Synth Portal")
	id, v := fd.Seed.Document(string(m.ID), lines)
	return pltbWorld{fd: fd, matter: string(m.ID), client: string(m.ClientID), doc: id, version: string(v)}
}

// pltbCall is one scripted model tool call.
type pltbCall struct {
	name  string
	input map[string]any
}

// pltbPlan is a report_plan call over steps.
func pltbPlan(steps ...string) pltbCall {
	var s []map[string]any
	for _, st := range steps {
		s = append(s, map[string]any{"step_id": st, "title": "Step " + st})
	}
	return pltbCall{agenttools.ToolReportPlan, map[string]any{"steps": s}}
}

// pltbScript is an inline aifake model for featureID: one tool call per turn, then text.
func pltbScript(t *testing.T, featureID string, calls []pltbCall, text string) *aifake.Fake {
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

// pltbToolNames lists the tool names of calls.
func pltbToolNames(calls []pltbCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// pltbDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func pltbDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: pltbDecideAt.Add(-time.Minute),
	}, by)
}

// pltbStatus is the HTTP status the API returns for a gate decision error.
func pltbStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// pltbCheckGate fails t unless gate id is pending, of kind, prepared by the agent for the
// acting user, and neither an agent principal (403) nor the acting user (ErrSelfApproval,
// 403) can decide it while another person can.
func pltbCheckGate(t *testing.T, fd *agenttest.FeatureDriver, id, kind string) {
	t.Helper()
	ctx := context.Background()
	g, err := fd.Seed.Gates.Get(ctx, gate.GateID(id))
	if err != nil {
		t.Fatal(err)
	}
	// rule: ARC-I-035
	if g.Kind != kind || g.Status != gate.StatusPending || g.Preparer.Kind != registry.PrincipalAgent || g.Preparer.ActingFor != agenttest.FeatureActingFor {
		t.Fatalf("gate = %s %s preparer %+v; want pending %s prepared for %s", g.Kind, g.Status, g.Preparer, kind, agenttest.FeatureActingFor)
	}
	tests := []struct {
		name       string
		by         registry.Principal
		wantErr    error
		wantStatus int
	}{
		// rule: ARC-I-039
		{"agent principal", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-" + id, ActingFor: pltbOtherLawyer}, gate.ErrAgentApprover, http.StatusForbidden},
		{"acting user decides own gate", registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, gate.ErrSelfApproval, http.StatusForbidden},
		{"another person", registry.Principal{Kind: registry.PrincipalUser, UserID: pltbOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pltbDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || pltbStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, pltbStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// pltbAbsent reports an error when text is in any write input of run or in the payload of
// any gate the run opened.
func pltbAbsent(ctx context.Context, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, text string) error {
	writes := map[string]bool{}
	for _, s := range fd.Feature.Tools {
		writes[s.Name] = s.Kind == agent.Write
	}
	for _, req := range run.Requests {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Type == "tool_use" && writes[b.ToolName] && strings.Contains(string(b.Input), text) {
					return fmt.Errorf("%s input carries the injected text", b.ToolName)
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
