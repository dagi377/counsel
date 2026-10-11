package matter_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/matter"
)

// rule: AI-R-003
// F-MAT-21 matches its spec/ai/features.json entry and a feature Set takes it once. Its one
// action is the one its trigger names.
func TestFMAT21_DefMatchesSpec(t *testing.T) {
	matgSpecSync(t, matter.AutoMapColumnsDef(), matter.ActionAutoMapColumns)
}

// F-MAT-21's prompt passes lint, sits under the shared rules preamble, and states the task
// and the expected output.
func TestFMAT21_Prompt(t *testing.T) {
	matgPrompt(t, matter.AutoMapColumnsDef(), "Auto-map columns", "auto-map columns on contact-import")
}

// F-MAT-21 runs only as the contact-import screen action: no schedule or event binds it.
func TestFMAT21_TriggerBinding(t *testing.T) {
	def := matter.AutoMapColumnsDef()
	_, bindErr := trigger.Bind(def, "")
	tests := []struct {
		field     string
		got, want string
	}{
		{"source", string(def.Source), string(feature.SourceScreenAction)},
		{"trigger", def.Trigger, "on request: contact-import > Auto-map columns"},
		{"actions", strings.Join(def.Actions, ";"), "contact-import > Auto-map columns"},
		{"no job binding", map[bool]string{true: "refused", false: "bound"}[errors.Is(bindErr, trigger.ErrBadBinding)], "refused"},
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
// F-MAT-21 reads the file and answers with a ranked mapping and flag rows cited to this
// run's reads; it writes nothing and opens no gate.
func TestFMAT21_FromFeature(t *testing.T) {
	matgReadOnly(t, matter.AutoMapColumnsDef(),
		"1. column 'Name' -> display_name (best), legal_name [1]. 2. column 'Reviewed' -> custom field review_date [1]. Flag: column 'Notes' has no field.")
}

// rule: AI-R-003
// The agent never approves: an agent principal deciding is 403, the acting user deciding
// their own gate is ErrSelfApproval.
func TestFMAT21_AgentNeverApproves(t *testing.T) {
	matgNoApproval(t, matter.AutoMapColumnsDef())
}
