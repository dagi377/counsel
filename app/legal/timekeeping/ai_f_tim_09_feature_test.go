package timekeeping_test

import (
	"testing"

	tk "github.com/dagi377/counsel/app/legal/timekeeping"
)

// rule: AI-R-003
func TestFTIM09_DefMatchesSpec(t *testing.T) {
	timdCheckDefMatchesSpec(t, tk.ExplainTrendDef())
}

// rule: AI-R-005
func TestFTIM09_Prompt(t *testing.T) {
	timdCheckPrompt(t, tk.ExplainTrendDef())
}

// F-TIM-09 runs only from its screen action, utilization > Explain trend.
func TestFTIM09_TriggerBinding(t *testing.T) {
	timdCheckScreenAction(t, tk.ExplainTrendDef(), "utilization > Explain trend")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFTIM09_FromFeature(t *testing.T) {
	timdCheckReadOnly(t, tk.ExplainTrendDef(), timdReadOnlyCase{
		action: "utilization > Explain trend",
		entries: []timdEntry{
			{minutes: 420, narrative: "Argue motion and prepare reply factum."},
		},
		answer: "Utilization rose this week: one 420-minute day of motion work drove it.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFTIM09_AgentNeverApproves(t *testing.T) {
	timdCheckNoApproval(t, tk.ExplainTrendDef())
}
