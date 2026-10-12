package platform_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/platform"
)

// rule: AI-R-003
func TestFPLT24_DefMatchesSpec(t *testing.T) {
	pltfCheckDefMatchesSpec(t, platform.ExplainChangeDef())
}

// rule: AI-R-005
func TestFPLT24_Prompt(t *testing.T) {
	pltjCheckPrompt(t, platform.ExplainChangeDef())
}

// F-PLT-24 runs only from its screen action, firm-insights > Explain change.
func TestFPLT24_TriggerBinding(t *testing.T) {
	pltjCheckScreenAction(t, platform.ExplainChangeDef(), "firm-insights > Explain change")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFPLT24_FromFeature(t *testing.T) {
	pltjCheckReadOnly(t, platform.ExplainChangeDef(), pltjReadOnlyCase{
		action: platform.ActionExplainChange,
		lines: []string{
			"Monthly insight note for the litigation group.",
			"Matter 2026-0615 closed on 2026-10-01, removing it from the open count.",
		},
		answer: "The open matter count fell because matter 2026-0615 closed.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFPLT24_AgentNeverApproves(t *testing.T) {
	pltjCheckNoApproval(t, platform.ExplainChangeDef())
}
