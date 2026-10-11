package work_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/work"
)

// rule: AI-R-003
func TestFWRK13_DefMatchesSpec(t *testing.T) {
	wrkdCheckSpec(t, work.PlainLanguageFilterDef())
}

func TestFWRK13_Prompt(t *testing.T) {
	wrkdCheckPrompt(t, work.PlainLanguageFilterDef(), "turn the user's sentence into a work item query", "Expected output: Ask in plain language on saved-filters")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFWRK13_FromFeature(t *testing.T) {
	wrkdCheckReadOnly(t, work.PlainLanguageFilterDef(), "Query: matter = 2026-0405 AND status = open. It matches the motion record review.")
}

// rule: AI-R-003
func TestFWRK13_AgentNeverApproves(t *testing.T) {
	wrkdCheckNoApproval(t, work.PlainLanguageFilterDef())
}
