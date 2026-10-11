package docs_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/docs"
)

// rule: AI-R-003
func TestFDOC21_DefMatchesSpec(t *testing.T) {
	dochCheckSpec(t, docs.FollowUpQuestionDef())
}

// rule: AI-R-005
func TestFDOC21_Prompt(t *testing.T) {
	dochCheckPrompt(t, docs.FollowUpQuestionDef(), "Follow-up question", "Follow-up answer on multi-document-analysis")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFDOC21_FromFeature(t *testing.T) {
	dochCheckReadOnly(t, docs.FollowUpQuestionDef(), "On the follow-up:")
}

// rule: ARC-I-039
func TestFDOC21_AgentNeverApproves(t *testing.T) {
	dochCheckNoApproval(t, docs.FollowUpQuestionDef())
}
