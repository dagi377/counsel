package intake_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/intake"
)

// rule: AI-R-003
func TestFINT13_DefMatchesSpec(t *testing.T) {
	inthCheckScreenSpec(t, intake.ProposeSlotsDef(), intake.ActionProposeSlots)
}

// rule: AI-R-005
func TestFINT13_Prompt(t *testing.T) {
	inthCheckPrompt(t, intake.ProposeSlotsDef(), "propose consultation slots for the selected lead", "critical leads get the earliest slots")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFINT13_FromFeature(t *testing.T) {
	inthCheckReadOnly(t, intake.ProposeSlotsDef(), "Earliest slot first: the lead is flagged critical.")
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFINT13_AgentNeverApproves(t *testing.T) {
	inthCheckNoApproval(t, intake.ProposeSlotsDef())
}
