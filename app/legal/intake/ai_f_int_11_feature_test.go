package intake_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/intake"
)

// rule: AI-R-003
func TestFINT11_DefMatchesSpec(t *testing.T) {
	inthCheckScreenSpec(t, intake.GuidedChatIntakeDef(), intake.ActionGuidedChatIntake)
}

// rule: AI-R-005
func TestFINT11_Prompt(t *testing.T) {
	inthCheckPrompt(t, intake.GuidedChatIntakeDef(), "Guided chat intake", "Never give legal advice")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFINT11_FromFeature(t *testing.T) {
	inthCheckReadOnly(t, intake.GuidedChatIntakeDef(), "Next question: what date did the incident happen?")
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFINT11_AgentNeverApproves(t *testing.T) {
	inthCheckNoApproval(t, intake.GuidedChatIntakeDef())
}
