package deadlines

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// rule: AI-R-003
// rule: ARC-I-035
// F-DL-04 matches its spec/ai/features.json entry: tools, gate kinds, model call, source and
// fixture, and boot can add it to a feature Set.
func TestFDL04_DefMatchesSpec(t *testing.T) {
	type specFeature struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Source    string   `json:"source"`
		Trigger   string   `json:"trigger"`
		Outputs   string   `json:"outputs"`
		Tools     []string `json:"tools"`
		ModelCall string   `json:"model_call"`
		Fixture   string   `json:"fixture"`
		Gate      struct {
			Kind []string `json:"kind"`
		} `json:"gate"`
	}
	var spec struct {
		Features []specFeature `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	d := ComplianceEvidenceMatcherDef()
	i := slices.IndexFunc(spec.Features, func(f specFeature) bool {
		return f.ID == d.ID
	})
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
	spec2, err := d.Spec()
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	// rule: AI-R-003
	// The run scope lets submit_for_approval open only the Def's gate kind.
	if !slices.Equal(spec2.GateKinds, []string{"cal.mark_met"}) {
		t.Fatalf("run gate kinds = %v, want [cal.mark_met]", spec2.GateKinds)
	}
	if err := s.Add(d); !errors.Is(err, feature.ErrBadDef) {
		t.Fatalf("second Add = %v, want ErrBadDef", err)
	}
}

// F-DL-04's prompt passes lint, sits under the shared rules preamble, and states the task and
// the expected output.
func TestFDL04_Prompt(t *testing.T) {
	d := ComplianceEvidenceMatcherDef()
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
	tests := []struct {
		name string
		want string
	}{
		{"preamble", feature.Preamble(d.ID)},
		{"shared rules", feature.Rules()},
		{"task", "Compliance evidence matcher"},
		{"expected output", "Proposal for G7d."},
		{"dates from compute_deadline", "compute_deadline"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(spec.System, tt.want) {
				t.Fatalf("system prompt lacks %q", tt.want)
			}
		})
	}
	if !strings.HasPrefix(spec.System, feature.Preamble(d.ID)) {
		t.Fatalf("system prompt does not start with the feature preamble")
	}
}

// F-DL-04 starts from the e-filing receipt, issued claim and affidavit of service events,
// with no schedule.
func TestFDL04_TriggerBinding(t *testing.T) {
	b, err := ComplianceEvidenceMatcherBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	tests := []struct {
		field     string
		got, want string
	}{
		{"feature", b.FeatureID, "F-DL-04"},
		{"events", strings.Join(b.Events, ","), "filing.receipt_received,claim.issued,service.affidavit_received"},
		{"cadence", string(b.Cadence), string(trigger.CadenceNone)},
		{"acting for", b.ActingFor, ""},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}
