package matter_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/matter"
)

// rule: AI-R-003
func TestFMAT23_DefMatchesSpec(t *testing.T) {
	mathCheckDefMatchesSpec(t, matter.SummarizeThisWeekDef())
}

// rule: AI-R-005
func TestFMAT23_Prompt(t *testing.T) {
	mathCheckPrompt(t, matter.SummarizeThisWeekDef())
}

// F-MAT-23 runs only from its screen action, firm-feed > Summarize this week.
func TestFMAT23_TriggerBinding(t *testing.T) {
	mathCheckScreenAction(t, matter.SummarizeThisWeekDef(), "firm-feed > Summarize this week")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFMAT23_FromFeature(t *testing.T) {
	mathCheckReadOnly(t, matter.SummarizeThisWeekDef(), mathReadOnlyCase{
		action: "firm-feed > Summarize this week",
		query:  "served",
		lines: []string{
			"Firm feed for the week, filtered to litigation.",
			"Notice of motion served 2026-10-01; reply record due next week.",
		},
		answer: "This week: one motion was served and a reply record is due next week.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFMAT23_AgentNeverApproves(t *testing.T) {
	mathCheckNoApproval(t, matter.SummarizeThisWeekDef())
}
