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
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// The intake features' end-to-end tests drive a Def with agenttest.FromFeatureWith over the
// app's real handlers, with no stand-ins: intcHandlers puts a draft store whose ids are a2
// uuids, and its subject lookup for submit_for_approval, ahead of the Seed's ports. Every
// record is synthetic.

// intcOtherLawyer is a second synthetic user who holds every gate kind's approver capability.
const intcOtherLawyer = "user-synthetic-intake-lawyer"

// intcDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var intcDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// intcDrafts stores drafts with deterministic uuid ids and maps each to its matter.
type intcDrafts struct {
	mu      sync.Mutex
	seq     int
	matters map[string]string
	drafts  []agent.NewDraftDocument
}

func newIntcDrafts() *intcDrafts { return &intcDrafts{matters: map[string]string{}} }

// intcDraftID is the id of the n-th draft a run stores.
func intcDraftID(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-00000000e%03d", n) }

// CreateDraft implements agent.DocumentDrafts.
func (s *intcDrafts) CreateDraft(_ context.Context, d agent.NewDraftDocument) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	id := intcDraftID(s.seq)
	s.matters[id] = d.MatterID
	s.drafts = append(s.drafts, d)
	return id, nil
}

// SubjectMatter implements agent.SubjectMatters.
func (s *intcDrafts) SubjectMatter(_ context.Context, _, id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.matters[id], nil
}

// list returns a copy of the stored drafts.
func (s *intcDrafts) list() []agent.NewDraftDocument {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.drafts)
}

// intcDepOf appends d's value of type T to vals, when d has one.
func intcDepOf[T any](vals []any, d agent.Deps) []any {
	if v, err := agent.DepOf[T](d); err == nil {
		return append(vals, v)
	}
	return vals
}

// intcHandlers is agent.Handlers() with each Build given extra ahead of the Seed's ports.
func intcHandlers(t *testing.T, extra ...any) agent.HandlerSet {
	t.Helper()
	app := agent.Handlers()
	var hs []agent.Handler
	for _, name := range app.Names() {
		h, _ := app.For(name)
		build := h.Build
		hs = append(hs, agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
			vals := slices.Clone(extra)
			vals = intcDepOf[agent.CapabilityCheck](vals, d)
			vals = intcDepOf[roles.Walls](vals, d)
			vals = intcDepOf[agenttools.Plans](vals, d)
			vals = intcDepOf[agenttools.GateOpener](vals, d)
			vals = intcDepOf[agenttools.RunScopes](vals, d)
			vals = intcDepOf[agent.DocumentText](vals, d)
			vals = intcDepOf[agent.ClientMatters](vals, d)
			vals = intcDepOf[agent.ContactSearch](vals, d)
			vals = intcDepOf[matter.Query](vals, d)
			vals = intcDepOf[agent.DocumentLookup](vals, d)
			vals = intcDepOf[agent.SubjectMatters](vals, d)
			vals = intcDepOf[*agenttools.Provenance](vals, d)
			vals = intcDepOf[*agenttest.SeedStore](vals, d)
			return build(agent.NewDeps(vals...))
		}})
	}
	set, err := agent.NewHandlerSet(hs...)
	if err != nil {
		t.Fatalf("handler set: %v", err)
	}
	return set
}

// intcApprovableGates gives the Seed a gate store in which the acting user and
// intcOtherLawyer hold the approver capability of every kind given, so the preparer rule, not
// a missing capability, is what stops the acting user.
func intcApprovableGates(fd *agenttest.FeatureDriver, kinds ...string) {
	caps := gatefake.Caps{}
	for _, k := range kinds {
		for _, u := range []string{agenttest.FeatureActingFor, intcOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return intcDecideAt }, caps)
	for _, k := range kinds {
		fd.Seed.GateKind(k)
	}
}

// intcFeature builds the FeatureDriver of def over the real handlers with an approvable gate
// store for the Def's gate kinds, and the draft store the run writes to.
func intcFeature(t *testing.T, def feature.Def, extra ...any) (*agenttest.FeatureDriver, *intcDrafts) {
	t.Helper()
	drafts := newIntcDrafts()
	fd := agenttest.FromFeatureWith(t, def, intcHandlers(t, append([]any{drafts}, extra...)...))
	intcApprovableGates(fd, def.GateKinds...)
	return fd, drafts
}

// intcDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func intcDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: intcDecideAt.Add(-time.Minute),
	}, by)
}

// intcStatus is the HTTP status the API returns for a gate decision error.
func intcStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// intcCheckGate fails t unless gate id is pending, of kind, prepared for the acting user, and
// neither an agent principal nor the acting user can decide it while another lawyer can.
func intcCheckGate(t *testing.T, fd *agenttest.FeatureDriver, id, kind string) {
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
		// rule: ARC-I-038
		{"agent principal", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-" + id, ActingFor: agenttest.FeatureActingFor}, gate.ErrAgentApprover, http.StatusForbidden},
		// rule: ARC-I-039
		{"acting user decides own gate", registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, gate.ErrSelfApproval, http.StatusForbidden},
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: intcOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := intcDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || intcStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, intcStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// intcCall is one scripted model tool call.
type intcCall struct {
	name  string
	input string // JSON
}

// intcScript is an inline aifake fixture for feature: one tool call per turn, then text.
func intcScript(feature, dir string, calls []intcCall, text string) fstest.MapFS {
	var b strings.Builder
	fmt.Fprintf(&b, "synthetic: true\nfeature: %s\nmatch:\n  last_user_regex: '.'\nturns:\n", feature)
	for _, c := range calls {
		fmt.Fprintf(&b, "  - reply:\n      tool_calls:\n        - name: %s\n          input: %s\n      stop_reason: tool_use\n", c.name, c.input)
	}
	fmt.Fprintf(&b, "  - reply:\n      text: %q\n      stop_reason: end_turn\n", text)
	return fstest.MapFS{strings.ToLower(feature) + "/" + dir + "/ok.yaml": {Data: []byte(b.String())}}
}

// intcToolNames lists the tool names of calls.
func intcToolNames(calls []intcCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// intcAbsent reports an error when text is in any write input of run or in the payload of any
// gate the run opened.
func intcAbsent(ctx context.Context, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, text string) error {
	kinds := map[string]bool{}
	for _, s := range fd.Feature.Tools {
		kinds[s.Name] = s.Kind == agent.Write
	}
	for _, req := range run.Requests {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Type == "tool_use" && kinds[b.ToolName] && strings.Contains(string(b.Input), text) {
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

// intcFeatureSpec is one spec/ai/features.json feature with its trigger and screen actions.
type intcFeatureSpec struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Source    string   `json:"source"`
	Trigger   string   `json:"trigger"`
	OnRequest []string `json:"on_request"`
	Tools     []string `json:"tools"`
	ModelCall string   `json:"model_call"`
	Fixture   string   `json:"fixture"`
	Gate      struct {
		Kind []string `json:"kind"`
	} `json:"gate"`
}

// intcCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry and a
// feature Set takes it once.
func intcCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	var spec struct {
		Features []intcFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f intcFeatureSpec) bool { return f.ID == d.ID })
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

// intcCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble,
// sits under the shared rules and holds every phrase in want.
func intcCheckPrompt(t *testing.T, d feature.Def, want ...string) {
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

// intcCheckBinding fails t unless b binds feature id to events, with no schedule, acting for
// the event's user.
func intcCheckBinding(t *testing.T, b trigger.Binding, err error, id string, events ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"feature", b.FeatureID, id},
		{"events", strings.Join(b.Events, ","), strings.Join(events, ",")},
		{"cadence", b.Cadence, trigger.CadenceNone},
		{"acting for", b.ActingFor, ""},
		{"designated", b.Designated, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}
