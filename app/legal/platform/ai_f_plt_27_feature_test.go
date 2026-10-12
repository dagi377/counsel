package platform_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/platform"
)

// rule: AI-R-003
func TestFPLT27_DefMatchesSpec(t *testing.T) {
	pltfCheckDefMatchesSpec(t, platform.ResearchDef())
}

// rule: AI-R-005
func TestFPLT27_Prompt(t *testing.T) {
	pltjCheckPrompt(t, platform.ResearchDef())
}

// F-PLT-27 runs only from its screen action, research-workspace > Research.
func TestFPLT27_TriggerBinding(t *testing.T) {
	pltjCheckScreenAction(t, platform.ResearchDef(), "research-workspace > Research")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFPLT27_FromFeature(t *testing.T) {
	pltjCheckReadOnly(t, platform.ResearchDef(), pltjReadOnlyCase{
		action: platform.ActionResearch,
		lines: []string{
			"Research memo on the limitation period.",
			"The basic limitation period ran from discovery on 2026-10-01.",
		},
		answer: "The basic limitation period runs from discovery.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFPLT27_AgentNeverApproves(t *testing.T) {
	pltjCheckNoApproval(t, platform.ResearchDef())
}
