package intake_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// Shared helpers of the intake features' tests. Every name carries the int prefix.

// intActingFor is the synthetic user an intake run acts for, and intOtherLawyer a second
// synthetic user who may decide its gates.
const (
	intActingFor   = "user-synthetic-intake-lawyer"
	intOtherLawyer = "user-synthetic-intake-reviewer"
)

// intDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var intDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// intFeatureSpec is one spec/ai/features.json feature.
type intFeatureSpec struct {
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

// intSpec returns id's spec/ai/features.json entry.
func intSpec(t *testing.T, id string) intFeatureSpec {
	t.Helper()
	var spec struct {
		Features []intFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f intFeatureSpec) bool { return f.ID == id })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", id)
	}
	return spec.Features[i]
}

// intKind is the Def kind a features.json model_call drives: "embedding+extraction" ranks by
// embedding and ends in one extraction call.
func intKind(modelCall string) ai.CallKind {
	switch modelCall {
	case "embedding+extraction":
		return ai.KindExtraction
	case "tool-use":
		return ai.KindToolUse
	}
	return ai.CallKind(modelCall)
}

// intCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry (the spec
// sync check for this id), a feature Set takes it once and its run scope is its gate kinds.
func intCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	sf := intSpec(t, d.ID)
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
		{"model call", string(kind), string(intKind(sf.ModelCall))},
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

// intCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble and
// the shared rules, and names the task and the expected output from features.json.
func intCheckPrompt(t *testing.T, d feature.Def) {
	t.Helper()
	sf := intSpec(t, d.ID)
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
	for _, w := range append([]string{"shared rules", sf.Name, sf.Outputs}, d.GateKinds...) {
		if !strings.Contains(spec.System, w) {
			t.Errorf("system prompt lacks %q", w)
		}
	}
}

// intRecorder records every model request.
type intRecorder struct {
	ai.ModelClient
	reqs []ai.Request
}

func (r *intRecorder) Complete(ctx context.Context, req ai.Request) (ai.Response, error) {
	r.reqs = append(r.reqs, req)
	return r.ModelClient.Complete(ctx, req)
}

// intScripted is an aifake model that answers featureID's one call with reply.
func intScripted(featureID, reply string, opts ...aifake.Option) *aifake.Fake {
	fx := "synthetic: true\nfeature: " + featureID + "\nmatch:\n  last_user_regex: \".\"\nturns:\n  - reply: " + reply + "\n"
	return aifake.New(fstest.MapFS{strings.ToLower(featureID) + "/script.yaml": {Data: []byte(fx)}}, opts...)
}

// intQuotedOnly fails unless text reached the model only inside a quoted, untrusted
// tool_result, never in the system prompt or user text.
func intQuotedOnly(t *testing.T, reqs []ai.Request, text string) {
	t.Helper()
	seen := false
	for _, req := range reqs {
		if strings.Contains(req.System, text) {
			t.Fatalf("source text in the system prompt")
		}
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if !strings.Contains(b.Text, text) && !strings.Contains(string(b.Input), text) {
					continue
				}
				var q agenttools.Quoted
				if b.Type != "tool_result" || json.Unmarshal([]byte(b.Text), &q) != nil || !q.Untrusted || q.Text != text {
					t.Fatalf("source text outside quoted data in a %s block", b.Type)
				}
				seen = true
			}
		}
	}
	if !seen {
		t.Fatalf("source text never reached the model")
	}
}

// intCaptureLogs sends the default logger to a buffer for the test.
func intCaptureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// intStatus is the HTTP status the API returns for a gate decision error.
func intStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// intCheckGate opens a gate of kind as an agent run acting for intActingFor would, then
// checks neither the agent principal nor the acting user can decide it (403) and another
// user holding approver can.
func intCheckGate(t *testing.T, kind, approver string) {
	t.Helper()
	ctx := context.Background()
	gates := gatefake.New(func() time.Time { return intDecideAt }, gatefake.Caps{
		intActingFor: {approver}, intOtherLawyer: {approver},
	})
	if err := gates.Register(gate.GateKind{
		Slug: kind, Approver: approver, Risk: gate.RiskHigh, MinApprovers: 1, TTL: 168 * time.Hour,
		Effect: func(context.Context, pgx.Tx, gate.Approved) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	agentP := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-synthetic", ActingFor: intActingFor}
	id, err := gates.Open(ctx, nil, kind, registry.Subject{Kind: "lead", ID: "lead-synthetic-1"}, json.RawMessage(`{"lead_id":"lead-synthetic-1","decision":"accept"}`), agentP)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		by         registry.Principal
		wantErr    error
		wantStatus int
	}{
		// rule: AI-R-003
		{"agent principal", agentP, gate.ErrAgentApprover, http.StatusForbidden},
		// rule: ARC-I-039
		{"acting user decides own gate", registry.Principal{Kind: registry.PrincipalUser, UserID: intActingFor}, gate.ErrSelfApproval, http.StatusForbidden},
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: intOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := gates.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			err = gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
				Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: intDecideAt.Add(-time.Minute),
			}, tt.by)
			if !errors.Is(err, tt.wantErr) || intStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, intStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}
