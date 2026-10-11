package deadlines

import "testing"

// rule: AI-R-003
func TestFDL12_DefMatchesSpec(t *testing.T) {
	checkSpec(t, ExplainStepDef())
}

func TestFDL12_Prompt(t *testing.T) {
	checkPrompt(t, ExplainStepDef(), "explain the current step of the rule-trigger wizard", "Explains in plain words which rule applies and why each excluded day was excluded.")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFDL12_FromFeature(t *testing.T) {
	checkReadOnly(t, ExplainStepDef(), "Rule R-17.01 applies to the motion record.")
}

// rule: AI-R-003
func TestFDL12_AgentNeverApproves(t *testing.T) {
	checkNoApproval(t, ExplainStepDef())
}
