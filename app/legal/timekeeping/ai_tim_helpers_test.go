package timekeeping_test

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
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/roles"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// Shared test helpers of the timekeeping AI features. Every name carries the tim prefix.

// timOtherLawyer is a lawyer other than the acting user, who may decide the run's gates.
const timOtherLawyer = "user-synthetic-other-lawyer"

// timDecideAt is the synthetic time gates are decided at.
var timDecideAt = time.Date(2026, 10, 5, 18, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// timToronto is the business time zone a work date is a calendar day in.
func timToronto(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// timMatterTime is list_unbilled's port: the priced entries the test seeds.
type timMatterTime struct{ entries []agent.PricedEntry }

func (m *timMatterTime) MatterTime(_ context.Context, matterID string) ([]agent.PricedEntry, error) {
	var out []agent.PricedEntry
	for _, p := range m.entries {
		if p.Entry.MatterID == matterID {
			out = append(out, p)
		}
	}
	return out, nil
}

// timEnv is what the real handlers read through beyond the agenttest Seed: the priced time
// list_unbilled reads, the entry store propose_time_entries suggests into, and the run's
// provenance (propose_time_entries declares its own sourced fields).
type timEnv struct {
	time       *timMatterTime
	entries    tk.Entries
	provenance *agenttools.Provenance
}

// timDepOf appends d's value of type T to vals, when d has one.
func timDepOf[T any](vals []any, d agent.Deps) []any {
	if v, err := agent.DepOf[T](d); err == nil {
		return append(vals, v)
	}
	return vals
}

// handlers is agent.Handlers() with each Build given the env's ports ahead of the Seed's.
// Each read tool records the values it returns into the run's provenance, as boot does.
func (e *timEnv) handlers(t *testing.T) agent.HandlerSet {
	t.Helper()
	app := agent.Handlers()
	var hs []agent.Handler
	for _, name := range app.Names() {
		h, _ := app.For(name)
		build := h.Build
		hs = append(hs, agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
			vals := []any{e.time, e.entries, e.provenance}
			vals = timDepOf[agent.CapabilityCheck](vals, d)
			vals = timDepOf[roles.Walls](vals, d)
			vals = timDepOf[agenttools.Plans](vals, d)
			vals = timDepOf[agenttools.GateOpener](vals, d)
			vals = timDepOf[agenttools.RunScopes](vals, d)
			vals = timDepOf[agent.WorkList](vals, d)
			vals = timDepOf[agent.ClientMatters](vals, d)
			vals = timDepOf[matter.Query](vals, d)
			vals = timDepOf[agent.ContactSearch](vals, d)
			vals = timDepOf[*agenttest.SeedStore](vals, d)
			tool, err := build(agent.NewDeps(vals...))
			if err != nil || tool.Kind != registry.ToolRead {
				return tool, err
			}
			return e.provenance.ReadTool(tool)
		}})
	}
	set, err := agent.NewHandlerSet(hs...)
	if err != nil {
		t.Fatalf("handler set: %v", err)
	}
	return set
}

// timApprovableGates gives the Seed a gate store in which the acting user and timOtherLawyer
// hold every gate kind's approver capability, so the preparer rule, not a missing capability,
// is what stops the acting user.
func timApprovableGates(fd *agenttest.FeatureDriver) {
	caps := gatefake.Caps{}
	for _, k := range fd.Def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, timOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return timDecideAt }, caps)
	for _, k := range fd.Def.GateKinds {
		fd.Seed.GateKind(k)
	}
}

// timDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func timDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: timDecideAt.Add(-time.Minute),
	}, by)
}

// timStatus is the HTTP status the API returns for a gate decision error.
func timStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// timCheckGate fails t unless gate id is pending, of kind, prepared for the acting user, and
// neither the agent principal (403) nor the acting user (ErrSelfApproval, 403) can decide it
// while another lawyer can.
func timCheckGate(t *testing.T, fd *agenttest.FeatureDriver, runID, id, kind string) {
	t.Helper()
	ctx := context.Background()
	g, err := fd.Seed.Gates.Get(ctx, gate.GateID(id))
	if err != nil {
		t.Fatal(err)
	}
	// rule: ARC-I-039
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
		{"agent principal", registry.Principal{Kind: registry.PrincipalAgent, RunID: runID, ActingFor: agenttest.FeatureActingFor}, gate.ErrAgentApprover, http.StatusForbidden},
		// rule: ARC-I-039
		{"acting user decides own gate", registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, gate.ErrSelfApproval, http.StatusForbidden},
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: timOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := timDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || timStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, timStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// timCall is one scripted model tool call.
type timCall struct {
	name  string
	input map[string]any
}

// timScript is an inline aifake model for featureID: one tool call per turn, then text.
func timScript(t *testing.T, featureID string, calls []timCall, text string) *aifake.Fake {
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

// timToolNames lists the tool names of calls.
func timToolNames(calls []timCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// timPlan is a report_plan call over steps.
func timPlan(steps ...string) timCall {
	var s []map[string]any
	for _, st := range steps {
		s = append(s, map[string]any{"step_id": st, "title": "Step " + st})
	}
	return timCall{agenttools.ToolReportPlan, map[string]any{"steps": s}}
}

// timAbsent reports an error when text is in any write input of run or in the payload of any
// gate the run opened.
func timAbsent(ctx context.Context, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, text string) error {
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

// timFeatureSpec is one spec/ai/features.json feature with its trigger and screen actions.
type timFeatureSpec struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Source    string   `json:"source"`
	Trigger   string   `json:"trigger"`
	OnRequest []string `json:"on_request"`
	Tools     []string `json:"tools"`
	ModelCall string   `json:"model_call"`
	Fixture   string   `json:"fixture"`
	Outputs   string   `json:"outputs"`
	Gate      struct {
		Kind []string `json:"kind"`
	} `json:"gate"`
}

// timSpec returns feature id's spec/ai/features.json entry.
func timSpec(t *testing.T, id string) timFeatureSpec {
	t.Helper()
	var spec struct {
		Features []timFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f timFeatureSpec) bool { return f.ID == id })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", id)
	}
	return spec.Features[i]
}

// timCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry and a
// feature Set takes it once.
func timCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	sf := timSpec(t, d.ID)
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
}

// timCheckPrompt lints d's prompt and checks the system prompt starts with the preamble and
// the shared rules and carries the feature's name, its output and want.
func timCheckPrompt(t *testing.T, d feature.Def, want ...string) {
	t.Helper()
	sf := timSpec(t, d.ID)
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
