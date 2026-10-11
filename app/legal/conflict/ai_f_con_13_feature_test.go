package conflict_test

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/kernel/ai"
)

// conCheckScreenSpec fails t unless the screen-action Def d matches its spec/ai/features.json
// entry, runs from action alone, is a chat feature (so the screen-action route answers inline
// and stores nothing) and a feature Set takes it once.
func conCheckScreenSpec(t *testing.T, d feature.Def, action string) {
	t.Helper()
	var spec struct {
		Features []conFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f conFeatureSpec) bool { return f.ID == d.ID })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", d.ID)
	}
	sf := spec.Features[i]
	sorted := func(s []string) string { s = slices.Clone(s); slices.Sort(s); return strings.Join(s, ",") }
	tests := []struct {
		field     string
		got, want string
	}{
		{"name", d.Name, sf.Name},
		{"source", string(d.Source), sf.Source},
		{"trigger", d.Trigger, sf.Trigger},
		{"screen action", "on request: " + strings.Join(d.Actions, ", "), sf.Trigger},
		{"action", strings.Join(d.Actions, ","), action},
		{"tools", sorted(d.Tools), sorted(sf.Tools)},
		{"gate kinds", sorted(d.GateKinds), sorted(sf.Gate.Kind)},
		// rule: AI-R-005
		// A chat feature runs inline on the screen-action route; its answer is never stored.
		{"model call", string(d.Kind), string(ai.KindChat)},
		{"spec model call", string(d.Kind), sf.ModelCall},
		{"fixture", d.Fixture, sf.Fixture},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
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

// rule: AI-R-003
func TestFCON13_DefMatchesSpec(t *testing.T) {
	conCheckScreenSpec(t, conflict.DetermineObligationDef(), conflict.ActionDetermineObligation)
}

// rule: AI-R-005
func TestFCON13_Prompt(t *testing.T) {
	conCheckReadOnlyPrompt(t, conflict.DetermineObligationDef(), "Determine obligation", "A proposal with a reason")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFCON13_FromFeature(t *testing.T) {
	conCheckReadOnly(t, conflict.DetermineObligationDef(), "Identify and verify: the matter involves receiving funds.")
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFCON13_AgentNeverApproves(t *testing.T) {
	conCheckNoApproval(t, conflict.DetermineObligationDef())
}
