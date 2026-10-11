package matter_test

import (
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/matter"
)

// rule: AI-R-003
// rule: ARC-I-035
// F-MAT-09 matches its spec/ai/features.json entry and a feature Set takes it once.
func TestFMAT09_DefMatchesSpec(t *testing.T) {
	matdSpecSync(t, matter.ConfigHygieneDef())
}

// F-MAT-09's prompt passes lint, sits under the shared rules preamble, and states the task
// and the expected output.
func TestFMAT09_Prompt(t *testing.T) {
	matdPrompt(t, matter.ConfigHygieneDef(), "Config hygiene", "Config proposals for G-CF")
}

// F-MAT-09 runs monthly for its designated lawyer, read from firm settings at each tick, and
// on request from practice-area settings.
func TestFMAT09_TriggerBinding(t *testing.T) {
	b, err := matter.ConfigHygieneBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	def := matter.ConfigHygieneDef()
	tests := []struct {
		field     string
		got, want string
	}{
		{"feature", b.FeatureID, "F-MAT-09"},
		{"cadence", string(b.Cadence), string(trigger.CadenceMonthly)},
		{"events", strings.Join(b.Events, ","), ""},
		{"designated", map[bool]string{true: "yes", false: "no"}[b.Designated], "yes"},
		{"acting for", b.ActingFor, ""},
		{"trigger", def.Trigger, "schedule: monthly"},
		{"on request", strings.Join(def.Actions, ";"), "practice-area-settings > Suggest starter set"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}

// rule: ARC-I-035
// rule: ARC-I-039
// rule: AI-R-003
// rule: AI-R-005
// The tool-gap kind matter.practice_config opens only through draft_document then
// submit_for_approval, with the acting user as preparer; no one in the run can decide it.
func TestFMAT09_FromFeature(t *testing.T) {
	matdFromFeature(t, matter.ConfigHygieneDef(), matter.GatePracticeConfig,
		"Config proposals: practice area", "Retire the unused intake stage [c1]. Make the closing checklist a template.",
		matdCases())
}
