package timekeeping_test

import (
	"testing"

	tk "github.com/dagi377/counsel/app/legal/timekeeping"
)

// rule: AI-R-003
func TestFTIM12_DefMatchesSpec(t *testing.T) {
	timdCheckDefMatchesSpec(t, tk.ExplainShortfallDef())
}

// rule: AI-R-005
func TestFTIM12_Prompt(t *testing.T) {
	timdCheckPrompt(t, tk.ExplainShortfallDef())
}

// F-TIM-12 runs only from its screen action, timekeeper-targets > Explain shortfall.
func TestFTIM12_TriggerBinding(t *testing.T) {
	timdCheckScreenAction(t, tk.ExplainShortfallDef(), "timekeeper-targets > Explain shortfall")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFTIM12_FromFeature(t *testing.T) {
	timdCheckReadOnly(t, tk.ExplainShortfallDef(), timdReadOnlyCase{
		action: "timekeeper-targets > Explain shortfall",
		entries: []timdEntry{
			{minutes: 60, narrative: "Review correspondence."},
		},
		answer: "Short of target: one 60-minute entry this week, and the open review task has no time.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFTIM12_AgentNeverApproves(t *testing.T) {
	timdCheckNoApproval(t, tk.ExplainShortfallDef())
}
