package timekeeping_test

import (
	"testing"

	tk "github.com/dagi377/counsel/app/legal/timekeeping"
)

// rule: AI-R-003
func TestFTIM10_DefMatchesSpec(t *testing.T) {
	timdCheckDefMatchesSpec(t, tk.FindInconsistenciesDef())
}

// rule: AI-R-005
func TestFTIM10_Prompt(t *testing.T) {
	timdCheckPrompt(t, tk.FindInconsistenciesDef())
}

// F-TIM-10 runs only from its screen action, activity-list > Find inconsistencies.
func TestFTIM10_TriggerBinding(t *testing.T) {
	timdCheckScreenAction(t, tk.FindInconsistenciesDef(), "activity-list > Find inconsistencies")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFTIM10_FromFeature(t *testing.T) {
	timdCheckReadOnly(t, tk.FindInconsistenciesDef(), timdReadOnlyCase{
		action: "activity-list > Find inconsistencies",
		entries: []timdEntry{
			{minutes: 360, narrative: "Draft motion record and review affidavits."},
			{minutes: 300, narrative: "Draft motion record and review affidavits."},
		},
		answer: "1. Duplicate narratives on entries 1 and 2. 2. One day over ten hours: 660 minutes.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFTIM10_AgentNeverApproves(t *testing.T) {
	timdCheckNoApproval(t, tk.FindInconsistenciesDef())
}
