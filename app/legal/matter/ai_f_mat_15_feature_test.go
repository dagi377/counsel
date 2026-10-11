package matter_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/matter"
)

// rule: AI-R-003
func TestFMAT15_DefMatchesSpec(t *testing.T) {
	mathCheckDefMatchesSpec(t, matter.ProposeNextActionsDef())
}

// rule: AI-R-005
func TestFMAT15_Prompt(t *testing.T) {
	mathCheckPrompt(t, matter.ProposeNextActionsDef())
}

// F-MAT-15 runs only from its screen action, matter-list > Propose next actions.
func TestFMAT15_TriggerBinding(t *testing.T) {
	mathCheckScreenAction(t, matter.ProposeNextActionsDef(), "matter-list > Propose next actions")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFMAT15_FromFeature(t *testing.T) {
	mathCheckReadOnly(t, matter.ProposeNextActionsDef(), mathReadOnlyCase{
		action: "matter-list > Propose next actions",
		query:  "filed",
		lines: []string{
			"Matter log, newest entry last.",
			"Statement of claim filed 2026-10-01; defence due in 20 days.",
		},
		answer: "1. Diarize the defence deadline. 2. Send the client a filing update.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFMAT15_AgentNeverApproves(t *testing.T) {
	mathCheckNoApproval(t, matter.ProposeNextActionsDef())
}
