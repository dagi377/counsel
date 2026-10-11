package docs_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/docs"
)

// rule: AI-R-003
func TestFDOC22_DefMatchesSpec(t *testing.T) {
	dochCheckSpec(t, docs.SuggestOrderAndTabsDef())
}

// rule: AI-R-005
func TestFDOC22_Prompt(t *testing.T) {
	dochCheckPrompt(t, docs.SuggestOrderAndTabsDef(), "Suggest order and tabs", "Suggested order and tabs on bundle-builder")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFDOC22_FromFeature(t *testing.T) {
	dochCheckReadOnly(t, docs.SuggestOrderAndTabsDef(), "Tab 1, the hearing notice:")
}

// rule: ARC-I-039
func TestFDOC22_AgentNeverApproves(t *testing.T) {
	dochCheckNoApproval(t, docs.SuggestOrderAndTabsDef())
}
