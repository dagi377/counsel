package deadlines

import "testing"

// rule: AI-R-003
func TestFDL10_DefMatchesSpec(t *testing.T) {
	checkSpec(t, ProposeMeetingDef())
}

func TestFDL10_Prompt(t *testing.T) {
	checkPrompt(t, ProposeMeetingDef(), "propose a meeting time for the matter team on the firm calendar", "Finds free time for the matter team.")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFDL10_FromFeature(t *testing.T) {
	checkReadOnly(t, ProposeMeetingDef(), "Two team slots clear the motion record.")
}

// rule: AI-R-003
func TestFDL10_AgentNeverApproves(t *testing.T) {
	checkNoApproval(t, ProposeMeetingDef())
}
