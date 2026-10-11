package matter_test

import (
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/matter"
)

// rule: AI-R-003
// F-MAT-13 matches its spec/ai/features.json entry and a feature Set takes it once. The
// matter-ask action is the one its trigger names.
func TestFMAT13_DefMatchesSpec(t *testing.T) {
	matgSpecSync(t, matter.MatterQuestionAnsweringDef(), matter.ActionAskMatter)
}

// F-MAT-13's prompt passes lint, sits under the shared rules preamble, and states the task
// and the expected output.
func TestFMAT13_Prompt(t *testing.T) {
	matgPrompt(t, matter.MatterQuestionAnsweringDef(), "Matter question answering", "cited answer; optional G-NT note draft")
}

// F-MAT-13 has no schedule and no event: it runs on request from matter-ask and from global
// search, both screen actions on its Def.
func TestFMAT13_TriggerBinding(t *testing.T) {
	def := matter.MatterQuestionAnsweringDef()
	tests := []struct {
		field     string
		got, want string
	}{
		{"trigger", def.Trigger, "on request from matter-ask"},
		{"on request", strings.Join(def.Actions, ";"), "matter-ask > Ask;global-search > Answer question with citations"},
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
// F-MAT-13 reads the matter and answers with citations of this run only; it writes nothing
// and opens no gate.
func TestFMAT13_FromFeature(t *testing.T) {
	matgReadOnly(t, matter.MatterQuestionAnsweringDef(), "The engagement note records an access review of matter 2026-0510 [1].")
}

// rule: AI-R-003
// The agent never approves: an agent principal deciding is 403, the acting user deciding
// their own gate is ErrSelfApproval.
func TestFMAT13_AgentNeverApproves(t *testing.T) {
	matgNoApproval(t, matter.MatterQuestionAnsweringDef())
}
