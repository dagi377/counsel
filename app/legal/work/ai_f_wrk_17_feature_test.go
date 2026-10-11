package work_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/work"
)

// rule: AI-R-003
func TestFWRK17_DefMatchesSpec(t *testing.T) {
	wrkeCheckDefMatchesSpec(t, work.ExplainNumberDef())
}

// rule: AI-R-005
func TestFWRK17_Prompt(t *testing.T) {
	wrkeCheckPrompt(t, work.ExplainNumberDef())
}

// F-WRK-17 runs only from its screen action, work-reports > Explain this number.
func TestFWRK17_TriggerBinding(t *testing.T) {
	wrkeCheckScreenAction(t, work.ExplainNumberDef(), "work-reports > Explain this number")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFWRK17_FromFeature(t *testing.T) {
	wrkeCheckReadOnly(t, work.ExplainNumberDef(), wrkeReadOnlyCase{
		action: "work-reports > Explain this number",
		lines: []string{
			"Weekly work report note.",
			"Overdue count rose 2026-10-01: three filing tasks passed their due dates.",
		},
		answer: "The overdue number rose because three filing tasks passed their due dates; see the listed items.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFWRK17_AgentNeverApproves(t *testing.T) {
	wrkeCheckNoApproval(t, work.ExplainNumberDef())
}
