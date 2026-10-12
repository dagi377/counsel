package platform_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/platform"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFPLT21_DefMatchesSpec(t *testing.T) {
	pltfCheckDefMatchesSpec(t, platform.PrepareTrustRefundDef())
}

// rule: AI-R-005
func TestFPLT21_Prompt(t *testing.T) {
	pltfCheckPrompt(t, platform.PrepareTrustRefundDef(), "Prepare trust refund / transfer",
		"prepare trust refund / transfer on matter-closing", "no money moves without lawyer authorization",
		agent.GateTrustWithdrawal, "overdraft", "integer cents", "report_plan", agent.ToolPrepareTrustWithdrawal)
}

// F-PLT-21 runs only on request from the matter-closing action, acting for the user who
// asked: it is no job, so it binds to no event or schedule.
func TestFPLT21_TriggerBinding(t *testing.T) {
	d := platform.PrepareTrustRefundDef()
	if d.Source != feature.SourceScreenAction || !slices.Equal(d.Actions, []string{platform.ActionPrepareTrustRefund}) {
		t.Fatalf("source %q actions %v, want screen-action %q", d.Source, d.Actions, platform.ActionPrepareTrustRefund)
	}
	if _, err := trigger.Bind(d, agenttest.FeatureActingFor, "matter.stage_changed"); !errors.Is(err, trigger.ErrBadBinding) {
		t.Fatalf("Bind = %v, want ErrBadBinding", err)
	}
}

// plt21Balance is the matter's cleared trust balance, in cents; the ledger statement on file
// states it.
const plt21Balance = 250_000

// plt21File is what the matter holds: the trust ledger statement and the closing report.
var plt21File = []string{
	"Trust ledger statement. Balance held for the client (cents): 250000. No cheque outstanding.",
	"Closing report: the matter concluded on 2026-10-02; the trust balance goes back to the client.",
}

const plt21RunID = "run-fplt21"

// plt21Run fires F-PLT-21 once from the matter-closing action over a matter whose file holds
// file and whose trust balance is plt21Balance; refund builds the withdrawal it prepares from
// the seeded matter.
func plt21Run(t *testing.T, file []string, refund func(pltfWorld) pltkRefund) (pltfWorld, agenttest.FeatureRun, []pltfCall, pltkRefund) {
	t.Helper()
	w := pltfFeature(t, platform.PrepareTrustRefundDef(), "2026-0321", "Closing Matter", file)
	w.fd.Seed.TrustBalance(w.matterID, plt21Balance)
	r := refund(w)
	calls := append(pltfReads(t, w, "trust ledger"), pltkTrustWrites(t, w, r)...)
	model := pltfScript("F-PLT-21", "prepare-trust-refund-transfer", calls, "Prepared the trust refund for a lawyer to authorize.")
	return w, w.fd.Fire(context.Background(), model, plt21RunID, "on request: "+platform.ActionPrepareTrustRefund), calls, r
}

// plt21ClientRefund refunds the balance the ledger states to the matter's client.
func plt21ClientRefund(w pltfWorld) pltkRefund {
	return pltkRefund{cents: plt21Balance, payee: w.clientID, purpose: "refund"}
}

// rule: ARC-I-035
// rule: ARC-I-039
// The refund is a withdrawal requested on the matter, to its client, waiting on
// trust.withdrawal prepared for the acting user; no money moved.
func TestFPLT21_FromFeature(t *testing.T) {
	w, run, calls, r := plt21Run(t, plt21File, plt21ClientRefund)
	pltkCheckTrustRun(t, w, run, calls, r, plt21Balance)
}

// rule: ARC-U-027
// By-Law 9: the real tool refuses, before anything is recorded, an amount no read of the run
// stated, a refund larger than the balance (an overdraft is never hidden behind a smaller
// figure, the request fails closed) and a payee that is not a contact of the matter.
func TestFPLT21_TrustRefusals(t *testing.T) {
	overdrawn := append(slices.Clone(plt21File), "Adjusted balance after the last receipt (cents): 900000.")
	tests := []struct {
		name   string
		file   []string
		refund func(pltfWorld) pltkRefund
		want   error
	}{
		{"amount no read stated", plt21File, func(w pltfWorld) pltkRefund {
			return pltkRefund{cents: 260_000, payee: w.clientID, purpose: "refund"}
		}, agenttools.ErrUnsourcedValue},
		{"more than the balance", overdrawn, func(w pltfWorld) pltkRefund {
			return pltkRefund{cents: 900_000, payee: w.clientID, purpose: "refund"}
		}, withdraw.ErrTrustOverdraw},
		{"payee not a matter contact", plt21File, func(pltfWorld) pltkRefund {
			return pltkRefund{cents: plt21Balance, payee: "cash", purpose: "refund"}
		}, agent.ErrPayeeNotContact},
		{"fees to the firm without a bill", plt21File, func(pltfWorld) pltkRefund {
			return pltkRefund{cents: plt21Balance, payee: agenttest.FirmPayee, purpose: "fees"}
		}, agent.ErrFeesNeedBill},
		{"refund to the firm", plt21File, func(pltfWorld) pltkRefund {
			return pltkRefund{cents: plt21Balance, payee: agenttest.FirmPayee, purpose: "refund"}
		}, agent.ErrFirmPayeeNeedsBill},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, run, _, _ := plt21Run(t, tt.file, tt.refund)
			pltkCheckRefused(t, w, run, tt.want, plt21Balance)
		})
	}
}

// rule: ARC-I-035
// submit_for_approval may open no kind outside the Def: the refund cannot be routed to the
// closing gate or any other.
func TestFPLT21_GateKindNotAllowed(t *testing.T) {
	w, _, _, _ := plt21Run(t, plt21File, plt21ClientRefund)
	calls := []pltfCall{
		{agenttools.ToolReportPlan, pltfPlan},
		{agent.ToolSubmitForApproval, pltfJSON(t, map[string]string{"subject_type": "document", "subject_id": w.docs[0].id, "gate_kind": "platform.closing"})},
	}
	model := pltfScript("F-PLT-21", "prepare-trust-refund-transfer", calls, "Done.")
	run := w.fd.Fire(context.Background(), model, plt21RunID+"-kind", "on request: "+platform.ActionPrepareTrustRefund)
	if len(run.Refused) != 1 || run.Refused[0].Tool != agent.ToolSubmitForApproval || !errors.Is(run.Refused[0].Err, agenttools.ErrGateKindNotAllowed) {
		t.Fatalf("refused = %v, want submit_for_approval refused with ErrGateKindNotAllowed", run.Refused)
	}
	if len(run.GateIDs) != 0 {
		t.Fatalf("gates opened: %v", run.GateIDs)
	}
}

// rule: ARC-I-038
func TestFPLT21_Injection(t *testing.T) {
	w, run, calls, _ := plt21Run(t, append(slices.Clone(plt21File), pltkInjected), plt21ClientRefund)
	pltkCheckInjection(t, w, run, calls, pltkInjected)
}
