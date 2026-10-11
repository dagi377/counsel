package access_test

import (
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// F-ACC-06 Offboarding plan (AI-017): on "Offboard started" the agent plans, finds the
// leaver's matters and drafts a handover that it submits for gate core.offboarding (GA12).
// The fixtures run through agenttest.FromFeature on the app's real handlers.

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestAI_R_002_ACC06_OffboardingPlan(t *testing.T) {
	want := []string{agenttools.ToolReportPlan, agent.ToolSearchMatters, agent.ToolDraftDocument, agent.ToolSubmitForApproval}
	accbLanded(t, access.OffboardingPlanDef(), "Offboard started for user-synthetic-leaver", want,
		func(t *testing.T, _ *agenttest.FeatureDriver, run agenttest.FeatureRun) {
			// Draft handover for GA12: the leaver's matter with its successor.
			title, body := accbDraftInput(t, run)
			if !strings.Contains(title, "Handover") || !strings.Contains(body, "successor") {
				t.Fatalf("draft = %q / %q, want the handover naming a successor", title, body)
			}
		})
}
