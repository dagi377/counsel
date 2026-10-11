package billing_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dagi377/counsel/app/legal/billing"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/fixture"
)

// F-BIL-07 Monthly billing digest (acceptance test AI-031): the monthly trigger runs the
// agent, which reads unbilled time, the bill and the trust balance and answers with an
// internal digest for lawyers. It is read-only: no gate opens and no write tool runs. The
// Def runs through agenttest.FromFeature over the real tool handlers: only the model is
// fake, answering from testdata/ai/f-bil-07.

const bil07Dir = "f-bil-07/monthly-billing-digest"

// rule: AI-R-002
// rule: AI-R-003
// rule: ARC-I-038
func TestAI_F_BIL_07_MonthlyBillingDigest(t *testing.T) {
	agenttest.Variants(t, billing.FeatureMonthlyDigest, bil07Dir)
	want := []string{agent.ToolListUnbilled, agent.ToolGetBill, agent.ToolGetTrustBalance}
	for _, variant := range fixture.RequiredVariants {
		t.Run(variant, func(t *testing.T) {
			ctx := context.Background()
			narrative := "Settlement review and report to client"
			if variant == fixture.VariantInjection {
				narrative += ". " + bilcInjected
			}
			def := billing.MonthlyDigestDef()
			fd := bilcFeature(t, def, narrative)
			model := agenttest.Model(t, bil07Dir, variant)
			runID, prompt := "run-ai031-"+variant, "Run the monthly billing digest for 2026-09."
			r := fd.Fire(ctx, model, runID, prompt)
			// rule: AI-R-003
			// A failed or rate-limited call leaves no partial report.
			switch variant {
			case fixture.VariantTimeout:
				if !errors.Is(r.Err, ai.ErrTimeout) || r.RetryQueued || len(r.Tools) != 0 || r.Text != "" {
					t.Fatalf("timeout: err %v queued %v calls %v text %q", r.Err, r.RetryQueued, r.Tools, r.Text)
				}
				return
			case fixture.VariantError429:
				if r.Err != nil || !r.RetryQueued || len(r.Tools) != 0 || r.Text != "" {
					t.Fatalf("429: err %v queued %v calls %v text %q", r.Err, r.RetryQueued, r.Tools, r.Text)
				}
				// The queued retry reruns the same run id and now reaches the script.
				r = fd.Fire(ctx, model, runID, prompt)
			}
			if r.Err != nil || r.RetryQueued || len(r.Refused) != 0 {
				t.Fatalf("run: err %v queued %v refused %v", r.Err, r.RetryQueued, r.Refused)
			}
			if !slices.Equal(r.Tools, want) || len(r.StandIns) != 0 {
				t.Fatalf("calls = %v (stand-ins %v), want %v", r.Tools, r.StandIns, want)
			}
			// Internal digest: no gate row and no write tool call.
			if len(r.GateIDs) != 0 || r.FirstWrite >= 0 {
				t.Fatalf("digest opened gates %v / wrote at %d; want none", r.GateIDs, r.FirstWrite)
			}
			spec, err := def.Spec()
			if err != nil {
				t.Fatal(err)
			}
			// rule: AI-R-005
			if _, err := run.CheckAnswer(spec, bilcOutcome(r)); err != nil {
				t.Fatalf("CheckAnswer: %v", err)
			}
			var ans struct {
				Answer string `json:"answer"`
			}
			if err := json.Unmarshal([]byte(r.Text), &ans); err != nil {
				t.Fatalf("answer: %v", err)
			}
			// rule: ARC-U-027
			if err := bilcSourcedAmounts(fd, runID, ans.Answer); err != nil {
				t.Fatalf("amounts: %v", err)
			}
			if variant == fixture.VariantInjection {
				if err := agenttest.QuotedOnly(r.Run, bilcInjected); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
