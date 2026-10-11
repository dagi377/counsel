package intake_test

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
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// The intf helpers drive the intake features F-INT-21 and F-INT-05 with agenttest.FromFeature
// over the app's real handlers and the Seed's synthetic records, with an inline aifake script.

// intfOtherLawyer is a second synthetic user who holds every gate kind's approver capability.
const intfOtherLawyer = "user-synthetic-intake-reviewer"

// intfDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var intfDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// intfFeature builds the FeatureDriver of def. Its gate store lets the acting user and
// intfOtherLawyer hold the approver capability of the Def's gate kinds, so the preparer rule,
// not a missing capability, is what stops the acting user.
func intfFeature(t *testing.T, def feature.Def) *agenttest.FeatureDriver {
	t.Helper()
	fd := agenttest.FromFeature(t, def)
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, intfOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return intfDecideAt }, caps)
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	return fd
}

// intfCall is one scripted model tool call.
type intfCall struct {
	name  string
	input string // JSON
}

// intfScript is an inline aifake fixture for feature: one tool call per turn, then text.
func intfScript(feature string, calls []intfCall, text string) fstest.MapFS {
	var b strings.Builder
	fmt.Fprintf(&b, "synthetic: true\nfeature: %s\nmatch:\n  last_user_regex: '.'\nturns:\n", feature)
	for _, c := range calls {
		fmt.Fprintf(&b, "  - reply:\n      tool_calls:\n        - name: %s\n          input: %s\n      stop_reason: tool_use\n", c.name, c.input)
	}
	fmt.Fprintf(&b, "  - reply:\n      text: %s\n      stop_reason: end_turn\n", intfYAML(text))
	return fstest.MapFS{strings.ToLower(feature) + "/inline/ok.yaml": {Data: []byte(b.String())}}
}

// intfYAML quotes s as a single-quoted YAML scalar.
func intfYAML(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// intfToolNames lists the tool names of calls.
func intfToolNames(calls []intfCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// intfDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func intfDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: intfDecideAt.Add(-time.Minute),
	}, by)
}

// intfStatus is the HTTP status the API returns for a gate decision error.
func intfStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// intfCheckGate fails t unless gate id is pending, of kind, prepared for the acting user, and
// neither an agent principal nor the acting user can decide it while another lawyer can.
func intfCheckGate(t *testing.T, fd *agenttest.FeatureDriver, id, kind string) {
	t.Helper()
	ctx := context.Background()
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
		// rule: ARC-I-039
		// The agent never approves.
		{"agent principal", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-" + id, ActingFor: agenttest.FeatureActingFor}, gate.ErrAgentApprover, http.StatusForbidden},
		// rule: ARC-I-039
		// The preparer never approves.
		{"acting user decides own gate", registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, gate.ErrSelfApproval, http.StatusForbidden},
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: intfOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := intfDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || intfStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, intfStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// intfAbsent reports an error when text is in any write input of r or in the payload of any
// gate the run opened.
func intfAbsent(ctx context.Context, fd *agenttest.FeatureDriver, r agenttest.FeatureRun, text string) error {
	writes := map[string]bool{}
	for _, s := range fd.Feature.Tools {
		writes[s.Name] = s.Kind == agent.Write
	}
	for _, req := range r.Requests {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Type == "tool_use" && writes[b.ToolName] && strings.Contains(string(b.Input), text) {
					return fmt.Errorf("write input carries the injected text: %s", b.Input)
				}
			}
		}
	}
	for _, id := range r.GateIDs {
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

var intfDate = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// intfSourcedDates returns agenttools.ErrUnsourcedValue when text names a date that no tool
// result of r returned as a value.
func intfSourcedDates(runID string, r agenttest.FeatureRun, text string) error {
	pv := agenttools.NewProvenance()
	for _, b := range r.ToolResults {
		if !b.IsError {
			pv.Observe(runID, json.RawMessage(b.Text))
		}
	}
	for _, d := range intfDate.FindAllString(text, -1) {
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

// intfOutcome is r as the job runner sees it: the final answer and every citation the run's
// quoted tool results carried (the "quotes" envelopes of search_contacts).
func intfOutcome(r agenttest.FeatureRun) run.Outcome {
	out := run.Outcome{Text: r.Text, JSON: r.JSON, GateIDs: r.GateIDs}
	for _, b := range r.ToolResults {
		var res struct {
			Quotes []agenttools.Quoted `json:"quotes"`
		}
		if b.IsError || json.Unmarshal([]byte(b.Text), &res) != nil {
			continue
		}
		for _, q := range res.Quotes {
			if q.Untrusted && q.Source.Page > 0 {
				out.Citations = append(out.Citations, q.Source)
			}
		}
	}
	return out
}

// intfFeatureSpec is one spec/ai/features.json feature.
type intfFeatureSpec struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Source    string   `json:"source"`
	Trigger   string   `json:"trigger"`
	Tools     []string `json:"tools"`
	ModelCall string   `json:"model_call"`
	Fixture   string   `json:"fixture"`
	Gate      struct {
		Kind []string `json:"kind"`
	} `json:"gate"`
}

// intfCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry (a screen
// action's trigger is "on request: " and its actions), a feature Set takes it once, and its
// run scope allows only its gate kinds.
func intfCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	var spec struct {
		Features []intfFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f intfFeatureSpec) bool { return f.ID == d.ID })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", d.ID)
	}
	sf := spec.Features[i]
	sorted := func(s []string) string { s = slices.Clone(s); slices.Sort(s); return strings.Join(s, ",") }
	trig := d.Trigger
	if d.Source == feature.SourceScreenAction {
		trig = "on request: " + strings.Join(d.Actions, ", ")
	}
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
		{"trigger", trig, sf.Trigger},
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

// intfCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble,
// sits under the shared rules and holds every phrase in want.
func intfCheckPrompt(t *testing.T, d feature.Def, want ...string) {
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
		t.Fatal("system prompt does not start with the feature preamble")
	}
	for _, w := range append([]string{feature.Rules()}, want...) {
		if !strings.Contains(spec.System, w) {
			t.Errorf("system prompt lacks %q", w)
		}
	}
}
