package platform_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/platform"
)

// rule: AI-R-003
func TestFPLT23_DefMatchesSpec(t *testing.T) {
	pltfCheckDefMatchesSpec(t, platform.DescribeReportDef())
}

// rule: AI-R-005
func TestFPLT23_Prompt(t *testing.T) {
	pltjCheckPrompt(t, platform.DescribeReportDef())
}

// F-PLT-23 runs only from its screen action, custom-report-builder > Describe report in words.
func TestFPLT23_TriggerBinding(t *testing.T) {
	pltjCheckScreenAction(t, platform.DescribeReportDef(), "custom-report-builder > Describe report in words")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFPLT23_FromFeature(t *testing.T) {
	pltjCheckReadOnly(t, platform.DescribeReportDef(), pltjReadOnlyCase{
		action: platform.ActionDescribeReport,
		lines: []string{
			"Reporting fields: matter number, responsible lawyer, open date.",
			"Matters opened since 2026-10-01 are in the current quarter.",
		},
		answer: "Dataset matters; columns matter number, responsible lawyer, open date; filter open date in the current quarter.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFPLT23_AgentNeverApproves(t *testing.T) {
	pltjCheckNoApproval(t, platform.DescribeReportDef())
}
