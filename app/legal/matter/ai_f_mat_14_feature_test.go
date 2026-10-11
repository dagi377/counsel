package matter_test

import (
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/matter"
)

// rule: AI-R-003
// rule: ARC-I-035
// F-MAT-14 matches its spec/ai/features.json entry and a feature Set takes it once.
func TestFMAT14_DefMatchesSpec(t *testing.T) {
	matdSpecSync(t, matter.TemplateFitDef())
}

// F-MAT-14's prompt passes lint, sits under the shared rules preamble, and states the task
// and the expected output.
func TestFMAT14_Prompt(t *testing.T) {
	matdPrompt(t, matter.TemplateFitDef(), "Template fit", "suggestion on matter-overview for G-AT")
}

// F-MAT-14 starts when a matter opens, weekly for its designated lawyer, read from firm
// settings at each tick, and on request from the matter overview.
func TestFMAT14_TriggerBinding(t *testing.T) {
	b, err := matter.TemplateFitBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	def := matter.TemplateFitDef()
	tests := []struct {
		field     string
		got, want string
	}{
		{"feature", b.FeatureID, "F-MAT-14"},
		{"events", strings.Join(b.Events, ","), "matter.opened"},
		{"cadence", string(b.Cadence), string(trigger.CadenceWeekly)},
		{"designated", map[bool]string{true: "yes", false: "no"}[b.Designated], "yes"},
		{"acting for", b.ActingFor, ""},
		{"trigger", def.Trigger, "event: matter opened without a template; weekly sweep"},
		{"on request", strings.Join(def.Actions, ";"), "matter-overview > Suggest a template"},
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
// The tool-gap kind matter.apply_template opens only through draft_document then
// submit_for_approval, with the acting user as preparer; no one in the run can decide it.
func TestFMAT14_FromFeature(t *testing.T) {
	matdFromFeature(t, matter.TemplateFitDef(), matter.GateApplyTemplate,
		"Template suggestion", "Apply the synthetic civil litigation template [c1].",
		matdCases())
}
