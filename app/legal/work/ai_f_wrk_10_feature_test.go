package work_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/work"
)

// rule: AI-R-003
func TestFWRK10_DefMatchesSpec(t *testing.T) {
	wrkdCheckSpec(t, work.AskQuestionDef())
}

func TestFWRK10_Prompt(t *testing.T) {
	wrkdCheckPrompt(t, work.AskQuestionDef(), "draft the question this work item needs a person to answer", "Expected output: Ask a question on work-item")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFWRK10_FromFeature(t *testing.T) {
	wrkdCheckReadOnly(t, work.AskQuestionDef(), "Draft for the assignee: is the motion record review on 2026-0405 still due as the order says?")
}

// rule: AI-R-003
func TestFWRK10_AgentNeverApproves(t *testing.T) {
	wrkdCheckNoApproval(t, work.AskQuestionDef())
}
