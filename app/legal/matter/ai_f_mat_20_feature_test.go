package matter_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/matter"
)

// rule: AI-R-003
func TestFMAT20_DefMatchesSpec(t *testing.T) {
	mathCheckDefMatchesSpec(t, matter.SuggestFromPastMattersDef())
}

// rule: AI-R-005
func TestFMAT20_Prompt(t *testing.T) {
	mathCheckPrompt(t, matter.SuggestFromPastMattersDef())
}

// F-MAT-20 runs only from its screen action, matter-templates > Suggest from past matters.
func TestFMAT20_TriggerBinding(t *testing.T) {
	mathCheckScreenAction(t, matter.SuggestFromPastMattersDef(), "matter-templates > Suggest from past matters")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFMAT20_FromFeature(t *testing.T) {
	mathCheckReadOnly(t, matter.SuggestFromPastMattersDef(), mathReadOnlyCase{
		action: "matter-templates > Suggest from past matters",
		query:  "closed",
		lines: []string{
			"Closing report for the area's litigation matters.",
			"Matter closed 2026-10-01 after pleadings, discovery and mediation stages.",
		},
		answer: "Preset 1: litigation with stages pleadings, discovery, mediation.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFMAT20_AgentNeverApproves(t *testing.T) {
	mathCheckNoApproval(t, matter.SuggestFromPastMattersDef())
}
