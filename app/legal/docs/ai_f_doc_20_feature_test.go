package docs_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/docs"
)

// rule: AI-R-003
func TestFDOC20_DefMatchesSpec(t *testing.T) {
	dochCheckSpec(t, docs.AnalyzeDef())
}

// rule: AI-R-005
func TestFDOC20_Prompt(t *testing.T) {
	dochCheckPrompt(t, docs.AnalyzeDef(), "Analyze", "Analysis on multi-document-analysis")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFDOC20_FromFeature(t *testing.T) {
	dochCheckReadOnly(t, docs.AnalyzeDef(), "Across the selected documents:")
}

// rule: ARC-I-039
func TestFDOC20_AgentNeverApproves(t *testing.T) {
	dochCheckNoApproval(t, docs.AnalyzeDef())
}
