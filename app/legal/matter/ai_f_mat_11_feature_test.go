package matter_test

import (
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/matter"
)

// rule: AI-R-003
// F-MAT-11 matches its spec/ai/features.json entry and a feature Set takes it once.
func TestFMAT11_DefMatchesSpec(t *testing.T) {
	matgSpecSync(t, matter.AccessHygieneDef())
}

// F-MAT-11's prompt passes lint, sits under the shared rules preamble, and states the task
// and the expected output.
func TestFMAT11_Prompt(t *testing.T) {
	matgPrompt(t, matter.AccessHygieneDef(), "Access hygiene", "flags on matter-access and matter-access-report")
}

// F-MAT-11 runs weekly for its designated lawyer, read from firm settings at each tick, and
// on request from the stage board, a matter's access screen and the access report.
func TestFMAT11_TriggerBinding(t *testing.T) {
	b, err := matter.AccessHygieneBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	def := matter.AccessHygieneDef()
	tests := []struct {
		field     string
		got, want string
	}{
		{"feature", b.FeatureID, "F-MAT-11"},
		{"cadence", string(b.Cadence), string(trigger.CadenceWeekly)},
		{"events", strings.Join(b.Events, ","), ""},
		{"designated", map[bool]string{true: "yes", false: "no"}[b.Designated], "yes"},
		{"acting for", b.ActingFor, ""},
		{"trigger", def.Trigger, "schedule: weekly"},
		{"on request", strings.Join(def.Actions, ";"),
			"matter-stage-board > Flag stuck matter;matter-access > Flag oversharing;matter-access-report > Flag over-shared matters"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
// F-MAT-11 reads and answers with cited flags; it writes nothing and opens no gate.
func TestFMAT11_FromFeature(t *testing.T) {
	matgReadOnly(t, matter.AccessHygieneDef(), "Matter 2026-0510 is visible to the whole firm; its area default is team only [1].")
}

// rule: AI-R-003
// The agent never approves: an agent principal deciding is 403, the acting user deciding
// their own gate is ErrSelfApproval.
func TestFMAT11_AgentNeverApproves(t *testing.T) {
	matgNoApproval(t, matter.AccessHygieneDef())
}
