package access_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// F-ACC-07 Invite list check (AI-018): on "list or CSV uploaded" the agent checks each invite
// row, records the row results and submits the ready rows for gate core.user_activation (GA2).
// The fixtures run through agenttest.FromFeature on the app's real handlers.

const accbInviteList = "Invite list uploaded lst-synth-001"

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestAI_R_002_ACC07_InviteListCheck(t *testing.T) {
	want := []string{agenttools.ToolReportPlan, agent.ToolSearchMatters, agent.ToolDraftDocument, agent.ToolSubmitForApproval}
	accbLanded(t, access.InviteListCheckDef(), accbInviteList, want,
		func(t *testing.T, _ *agenttest.FeatureDriver, run agenttest.FeatureRun) {
			// Row results on user-list; rows 1 and 3 ready for GA2, row 2 a duplicate.
			_, body := accbDraftInput(t, run)
			for _, w := range []string{"Row 1 ada.synth@example.test: ready", "Row 2 bo.synth@example.test: duplicate", "Row 3 cy.synth@example.test: ready"} {
				if !strings.Contains(body, w) {
					t.Fatalf("row results = %q, want %q", body, w)
				}
			}
		})
}

// rule: AI-R-003
// An extraction output that is not a row list records nothing: no draft and no gate.
func TestAI_R_002_ACC07_BadOutput(t *testing.T) {
	def := access.InviteListCheckDef()
	fd := accbFeature(t, def, accbMatterName(false))
	run := fd.Fire(context.Background(), agenttest.Model(t, "f-acc-07/invite-list-check", "bad_output"), "run-f-acc-07-bad", accbInviteList+" [bad_output]")
	if len(run.Tools) != 0 || len(run.GateIDs) != 0 || len(fd.Seed.Store.Drafts) != 0 {
		t.Fatalf("partial write: calls %v gates %v drafts %+v", run.Tools, run.GateIDs, fd.Seed.Store.Drafts)
	}
}
