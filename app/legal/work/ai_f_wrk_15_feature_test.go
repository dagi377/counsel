package work_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/work"
)

// rule: AI-R-003
func TestFWRK15_DefMatchesSpec(t *testing.T) {
	wrkeCheckDefMatchesSpec(t, work.ExplainFailedRunDef())
}

// rule: AI-R-005
func TestFWRK15_Prompt(t *testing.T) {
	wrkeCheckPrompt(t, work.ExplainFailedRunDef())
}

// F-WRK-15 runs only from its screen action, work-automations > Explain failed or skipped run.
func TestFWRK15_TriggerBinding(t *testing.T) {
	wrkeCheckScreenAction(t, work.ExplainFailedRunDef(), "work-automations > Explain failed or skipped run")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFWRK15_FromFeature(t *testing.T) {
	wrkeCheckReadOnly(t, work.ExplainFailedRunDef(), wrkeReadOnlyCase{
		action: "work-automations > Explain failed or skipped run",
		lines: []string{
			"Run log for the intake automation.",
			"Run skipped 2026-10-01: template engagement-letter is retired.",
		},
		answer: "The run was skipped because its engagement-letter template is retired; pick a current template in the rule.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFWRK15_AgentNeverApproves(t *testing.T) {
	wrkeCheckNoApproval(t, work.ExplainFailedRunDef())
}
