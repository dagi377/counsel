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
// F-DL-06 matches its spec/ai/features.json entry: tools, gate kinds, model call, source and
// fixture, and boot can add it to a feature Set.
func TestFDL06_DefMatchesSpec(t *testing.T) {
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
	d := RuleWatchDef()
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
	if !slices.Equal(spec2.GateKinds, []string{"cal.rule_pack"}) {
		t.Fatalf("run gate kinds = %v, want [cal.rule_pack]", spec2.GateKinds)
	}
	if err := s.Add(d); !errors.Is(err, feature.ErrBadDef) {
		t.Fatalf("second Add = %v, want ErrBadDef", err)
	}
}

// F-DL-06's prompt passes lint, sits under the shared rules preamble, and states the task and
// the expected output.
func TestFDL06_Prompt(t *testing.T) {
	d := RuleWatchDef()
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
		{"task", "Rule watch"},
		{"expected output", "Draft rule-pack version for G7e."},
		{"tool-gap path", "draft_document"},
		{"gate opened by submit", "submit_for_approval"},
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

// F-DL-06 runs weekly for the user boot names; with no acting user the binding is refused.
func TestFDL06_TriggerBinding(t *testing.T) {
	tests := []struct {
		name      string
		actingFor string
		wantErr   error
	}{
		{"weekly for the rule-pack admin", "u-rulepack-admin", nil},
		{"no acting user", "", trigger.ErrBadBinding},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := RuleWatchBinding(tt.actingFor)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Binding err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if b.FeatureID != "F-DL-06" || b.Cadence != trigger.CadenceWeekly || len(b.Events) != 0 || b.ActingFor != tt.actingFor {
				t.Fatalf("binding = %+v, want F-DL-06 weekly for %s with no events", b, tt.actingFor)
			}
		})
	}
}
