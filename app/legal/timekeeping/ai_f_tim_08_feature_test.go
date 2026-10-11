package timekeeping_test

import (
	"testing"

	tk "github.com/dagi377/counsel/app/legal/timekeeping"
)

// rule: AI-R-003
func TestFTIM08_DefMatchesSpec(t *testing.T) {
	timdCheckDefMatchesSpec(t, tk.FlagOutOfScopeDef())
}

// rule: AI-R-005
func TestFTIM08_Prompt(t *testing.T) {
	timdCheckPrompt(t, tk.FlagOutOfScopeDef())
}

// F-TIM-08 runs only from its screen action, matter-fee-arrangement > Flag out-of-scope work.
func TestFTIM08_TriggerBinding(t *testing.T) {
	timdCheckScreenAction(t, tk.FlagOutOfScopeDef(), "matter-fee-arrangement > Flag out-of-scope work")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFTIM08_FromFeature(t *testing.T) {
	timdCheckReadOnly(t, tk.FlagOutOfScopeDef(), timdReadOnlyCase{
		action: "matter-fee-arrangement > Flag out-of-scope work",
		entries: []timdEntry{
			{minutes: 240, narrative: "Prepare notice of appeal and appeal book."},
		},
		answer: "Out of scope: entry 1, a new appeal, is outside the flat-fee trial scope.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFTIM08_AgentNeverApproves(t *testing.T) {
	timdCheckNoApproval(t, tk.FlagOutOfScopeDef())
}
