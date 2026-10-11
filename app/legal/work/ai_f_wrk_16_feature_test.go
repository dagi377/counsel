package work_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/work"
)

// rule: AI-R-003
func TestFWRK16_DefMatchesSpec(t *testing.T) {
	wrkeCheckDefMatchesSpec(t, work.SuggestCategoriesDef())
}

// rule: AI-R-005
func TestFWRK16_Prompt(t *testing.T) {
	wrkeCheckPrompt(t, work.SuggestCategoriesDef())
}

// F-WRK-16 runs only from its screen action, work-categories > Suggest categories.
func TestFWRK16_TriggerBinding(t *testing.T) {
	wrkeCheckScreenAction(t, work.SuggestCategoriesDef(), "work-categories > Suggest categories")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFWRK16_FromFeature(t *testing.T) {
	wrkeCheckReadOnly(t, work.SuggestCategoriesDef(), wrkeReadOnlyCase{
		action: "work-categories > Suggest categories",
		lines: []string{
			"Work plan for the matter.",
			"Items opened 2026-10-01: draft pleadings, serve claim, book discovery.",
		},
		answer: "Suggested categories: Pleadings (draft pleadings), Service (serve claim), Discovery (book discovery).",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFWRK16_AgentNeverApproves(t *testing.T) {
	wrkeCheckNoApproval(t, work.SuggestCategoriesDef())
}
