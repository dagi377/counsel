package access_test

import (
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// F-ACC-10 Suggest team (AI-021): on matter-access > Suggest team the agent proposes the
// matter's team from the practice area and workload and submits it for gate
// core.matter_visibility. The fixtures run through agenttest.FromFeature on the app's real
// handlers.

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestAI_R_002_ACC10_SuggestTeam(t *testing.T) {
	want := []string{agenttools.ToolReportPlan, agent.ToolSearchMatters, agent.ToolDraftDocument, agent.ToolSubmitForApproval}
	accbLanded(t, access.SuggestTeamDef(), "matter-access > Suggest team: matter "+accbMatterID, want,
		func(t *testing.T, _ *agenttest.FeatureDriver, run agenttest.FeatureRun) {
			// Suggest team from practice area and workload.
			_, body := accbDraftInput(t, run)
			for _, w := range []string{"litigation", "open matters"} {
				if !strings.Contains(body, w) {
					t.Fatalf("team = %q, want it to give %q", body, w)
				}
			}
		})
}
