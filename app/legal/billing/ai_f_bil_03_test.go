package billing

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/fixture"
)

// F-BIL-03 Trust application proposal (acceptance test AI-027): on "bill delivered and
// retainer permits" the agent plans, reads the bill and the trust balance and prepares a
// transfer proposal for G-B4. The Def runs through agenttest.FromFeature over the real tool
// handlers: only the model is fake, answering from testdata/ai/f-bil-03.

const bil03Dir = "f-bil-03/trust-application-proposal"

// rule: AI-R-002
// rule: AI-R-003
// rule: ARC-I-038
func TestAI_R_002_BIL03_TrustApplicationProposal(t *testing.T) {
	agenttest.Variants(t, FeatureTrustApplication, bil03Dir)
	want := []string{agent.ToolReportPlan, agent.ToolGetBill, agent.ToolGetTrustBalance, agent.ToolPrepareTrustWithdrawal}
	for _, variant := range fixture.RequiredVariants {
		t.Run(variant, func(t *testing.T) {
			ctx := context.Background()
			narrative := "Settlement review and report to client"
			if variant == fixture.VariantInjection {
				narrative += ". " + bilcInjected
			}
			fd := bilcFeature(t, TrustApplicationDef(), narrative)
			model := agenttest.Model(t, bil03Dir, variant)
			runID, prompt := "run-ai027-"+variant, "Bill "+bilcBill+" delivered; retainer permits ["+variant+"]"
			run := fd.Fire(ctx, model, runID, prompt)
			// rule: AI-R-003
			// A failed or rate-limited call leaves no partial write.
			switch variant {
			case fixture.VariantTimeout:
				if !errors.Is(run.Err, ai.ErrTimeout) || run.RetryQueued || len(run.Tools) != 0 || len(run.GateIDs) != 0 {
					t.Fatalf("timeout: err %v queued %v calls %v gates %v", run.Err, run.RetryQueued, run.Tools, run.GateIDs)
				}
				return
			case fixture.VariantError429:
				if run.Err != nil || !run.RetryQueued || len(run.Tools) != 0 || len(run.GateIDs) != 0 {
					t.Fatalf("429: err %v queued %v calls %v gates %v", run.Err, run.RetryQueued, run.Tools, run.GateIDs)
				}
				// The queued retry reruns the same run id and now reaches the script.
				run = fd.Fire(ctx, model, runID, prompt)
			}
			if run.Err != nil || run.RetryQueued || len(run.Refused) != 0 {
				t.Fatalf("run: err %v queued %v refused %v", run.Err, run.RetryQueued, run.Refused)
			}
			if !slices.Equal(run.Tools, want) || len(run.StandIns) != 0 {
				t.Fatalf("calls = %v (stand-ins %v), want %v", run.Tools, run.StandIns, want)
			}
			// rule: ARC-I-039
			if run.PlanAt != 0 || run.FirstWrite != 3 {
				t.Fatalf("report_plan at %d, first write at %d", run.PlanAt, run.FirstWrite)
			}
			bil03Proposal(t, fd, run.Run)
			if variant == fixture.VariantInjection {
				if err := agenttest.QuotedOnly(run.Run, bilcInjected); err != nil {
					t.Fatal(err)
				}
				if bilcCarries(fd, run.GateIDs, bilcInjected) {
					t.Fatal("injected text reached the proposal")
				}
			}
			if run.Text == "" {
				t.Fatal("no final answer")
			}
		})
	}
}
