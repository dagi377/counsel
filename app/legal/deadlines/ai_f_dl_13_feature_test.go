package deadlines

import "testing"

// rule: AI-R-003
func TestFDL13_DefMatchesSpec(t *testing.T) {
	checkSpec(t, DraftAgendaDef())
}

func TestFDL13_Prompt(t *testing.T) {
	checkPrompt(t, DraftAgendaDef(), "draft the agenda of the selected event", "From the matter brief and open work items.")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFDL13_FromFeature(t *testing.T) {
	checkReadOnly(t, DraftAgendaDef(), "Agenda: 1. Motion record.")
}

// rule: AI-R-003
func TestFDL13_AgentNeverApproves(t *testing.T) {
	checkNoApproval(t, DraftAgendaDef())
}
