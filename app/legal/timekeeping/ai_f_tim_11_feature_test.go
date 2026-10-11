package timekeeping_test

import (
	"testing"

	tk "github.com/dagi377/counsel/app/legal/timekeeping"
)

// rule: AI-R-003
func TestFTIM11_DefMatchesSpec(t *testing.T) {
	timdCheckDefMatchesSpec(t, tk.SuggestCategoriesDef())
}

// rule: AI-R-005
func TestFTIM11_Prompt(t *testing.T) {
	timdCheckPrompt(t, tk.SuggestCategoriesDef())
}

// F-TIM-11 runs only from its screen action, activity-categories > Suggest categories from history.
func TestFTIM11_TriggerBinding(t *testing.T) {
	timdCheckScreenAction(t, tk.SuggestCategoriesDef(), "activity-categories > Suggest categories from history")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFTIM11_FromFeature(t *testing.T) {
	timdCheckReadOnly(t, tk.SuggestCategoriesDef(), timdReadOnlyCase{
		action: "activity-categories > Suggest categories from history",
		entries: []timdEntry{
			{minutes: 90, narrative: "Draft statement of claim."},
			{minutes: 30, narrative: "Call with client about settlement."},
		},
		answer: "Proposed categories: 1. Drafting (pleadings and motions). 2. Client calls.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFTIM11_AgentNeverApproves(t *testing.T) {
	timdCheckNoApproval(t, tk.SuggestCategoriesDef())
}
