package docs_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/docs"
)

// rule: AI-R-003
func TestFDOC14_DefMatchesSpec(t *testing.T) {
	dochCheckSpec(t, docs.AnswerWithCitationsDef())
}

// rule: AI-R-005
func TestFDOC14_Prompt(t *testing.T) {
	dochCheckPrompt(t, docs.AnswerWithCitationsDef(), "Answer with citations", "Answer with citations on document-search")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFDOC14_FromFeature(t *testing.T) {
	dochCheckReadOnly(t, docs.AnswerWithCitationsDef(), "From the hits:")
}

// rule: ARC-I-039
func TestFDOC14_AgentNeverApproves(t *testing.T) {
	dochCheckNoApproval(t, docs.AnswerWithCitationsDef())
}
