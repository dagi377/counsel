package deadlines

import "testing"

// rule: AI-R-003
func TestFDL11_DefMatchesSpec(t *testing.T) {
	checkSpec(t, FindTeamTimeDef())
}

func TestFDL11_Prompt(t *testing.T) {
	checkPrompt(t, FindTeamTimeDef(), "find a time for the matter team on the firm calendar", "Already partly in 'Propose meeting time'; also checks court holidays.")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFDL11_FromFeature(t *testing.T) {
	checkReadOnly(t, FindTeamTimeDef(), "One team time clears the court holidays.")
}

// rule: AI-R-003
func TestFDL11_AgentNeverApproves(t *testing.T) {
	checkNoApproval(t, FindTeamTimeDef())
}
