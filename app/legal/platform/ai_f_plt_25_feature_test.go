package platform_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/platform"
)

// rule: AI-R-003
func TestFPLT25_DefMatchesSpec(t *testing.T) {
	pltfCheckDefMatchesSpec(t, platform.SummarizeThreadDef())
}

// rule: AI-R-005
func TestFPLT25_Prompt(t *testing.T) {
	pltjCheckPrompt(t, platform.SummarizeThreadDef())
}

// F-PLT-25 runs only from its screen action, internal-messages > Summarize thread.
func TestFPLT25_TriggerBinding(t *testing.T) {
	pltjCheckScreenAction(t, platform.SummarizeThreadDef(), "internal-messages > Summarize thread")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFPLT25_FromFeature(t *testing.T) {
	pltjCheckReadOnly(t, platform.SummarizeThreadDef(), pltjReadOnlyCase{
		action: platform.ActionSummarizeThread,
		lines: []string{
			"Thread: settlement position for the mediation.",
			"Agreed to send the revised offer by 2026-10-01; the associate owns the draft.",
		},
		answer: "The team agreed to send a revised offer; the associate owns the draft.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFPLT25_AgentNeverApproves(t *testing.T) {
	pltjCheckNoApproval(t, platform.SummarizeThreadDef())
}
