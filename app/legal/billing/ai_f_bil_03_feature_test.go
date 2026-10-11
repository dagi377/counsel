package billing_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dagi377/counsel/app/legal/billing"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// bil03Withdraw is the prepare_trust_withdrawal call of an F-BIL-03 script for amount cents
// paid to payee.
func bil03Withdraw(cents int64, payee string) bilcCall {
	return bilcCall{name: agent.ToolPrepareTrustWithdrawal, input: `{"matter_id":"` + agenttest.TrustMatter +
		`","bill_id":"` + bilcBill + `","amount_cents":` + strconv.FormatInt(cents, 10) +
		`,"purpose_category":"fees","payee":"` + payee + `"}`}
}

// rule: ARC-I-035
// rule: ARC-I-039
// rule: ARC-U-027
// rule: AI-R-003
func TestFBIL03_FromFeature(t *testing.T) {
	calls := func(cents int64, payee string) []bilcCall {
		return append([]bilcCall{bilcPlan("bill", "balance", "propose")}, append(slices.Clone(bilcReads), bil03Withdraw(cents, payee))...)
	}
	cases := []struct {
		name      string
		narrative string
		cents     int64
		payee     string // empty: the firm's general account (agenttest.FirmPayee)
		wantErr   error  // the refusal of prepare_trust_withdrawal; nil: one pending proposal
	}{
		// rule: BIL-L-037
		// A fee transfer pays the firm's general account for a delivered bill (By-Law 9).
		{name: "ok", narrative: "Settlement review and report to client", cents: bilcTrust},
		// rule: BIL-L-037
		// A fee transfer for a bill never pays a matter contact.
		{name: "contact payee for fees", narrative: "Settlement review", cents: bilcTrust, payee: agenttest.PayeeContact, wantErr: agent.ErrFeesNeedFirmPayee},
		// rule: ARC-U-027
		// A model-invented amount is refused before any withdrawal row exists.
		{name: "model-invented amount", narrative: "Settlement review", cents: bilcTrust - 100, wantErr: agenttools.ErrUnsourcedValue},
		// rule: BIL-L-037
		// The bill balance is a sourced figure, but it overdraws trust: refused, never hidden.
		{name: "overdraft", narrative: "Settlement review", cents: bilcBillTotal, wantErr: withdraw.ErrTrustOverdraw},
		// rule: ARC-I-038
		{name: "injection", narrative: "Settlement review. " + bilcInjected, cents: bilcTrust},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			def := billing.TrustApplicationDef()
			fd := bilcFeature(t, def, c.narrative)
			payee := c.payee
			if payee == "" {
				payee = agenttest.FirmPayee
			}
			script := calls(c.cents, payee)
			run := fd.Fire(ctx, bilcScript(t, def.ID, script, "Prepared the trust-to-fees transfer for approval."),
				"run-fbil03-"+strings.ReplaceAll(c.name, " ", "-"), "Bill "+bilcBill+" delivered; propose a transfer from trust.")
			if run.Err != nil {
				t.Fatalf("run: %v", run.Err)
			}
			// rule: ARC-I-038
			if !slices.Equal(run.Tools, bilcNames(script)) {
				t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, bilcNames(script))
			}
			if c.wantErr != nil {
				if len(run.Refused) != 1 || run.Refused[0].Tool != agent.ToolPrepareTrustWithdrawal || !errors.Is(run.Refused[0].Err, c.wantErr) {
					t.Fatalf("refused = %v, want prepare_trust_withdrawal refused with %v", run.Refused, c.wantErr)
				}
				if _, err := fd.Withdrawals.Get(ctx, "wd_1"); err == nil || len(run.GateIDs) != 0 {
					t.Fatalf("refused amount left a withdrawal row or gates %v", run.GateIDs)
				}
				return
			}
			if len(run.Refused) != 0 {
				t.Fatalf("refused = %v", run.Refused)
			}
			// rule: ARC-I-039
			if run.PlanAt != 0 || run.FirstWrite != len(script)-1 {
				t.Fatalf("plan at %d, first write at %d: plan must come first", run.PlanAt, run.FirstWrite)
			}
			bil03Proposal(t, fd, run.Run)
			if c.name == "injection" {
				if err := agenttest.QuotedOnly(run.Run, bilcInjected); err != nil {
					t.Fatal(err)
				}
				if bilcCarries(fd, run.GateIDs, bilcInjected) {
					t.Fatal("injected text reached the proposal")
				}
			}
		})
	}
}

// bil03Proposal checks the run's one proposal: a requested withdrawal of the trust balance
// for bilcBill, prepared by the acting user, that opens billing.trust_fee_withdrawal, which
// neither an agent principal nor the acting user may decide.
func bil03Proposal(t *testing.T, fd *agenttest.FeatureDriver, run agenttest.Run) {
	t.Helper()
	ctx := context.Background()
	if len(run.GateIDs) != 1 {
		t.Fatalf("gates = %v, want one", run.GateIDs)
	}
	p, ok := fd.Proposal(run.GateIDs[0])
	// rule: ARC-I-035
	if !ok || !p.Real || p.Preparer != agenttest.FeatureActingFor || p.Decision != "" {
		t.Fatalf("proposal = %+v, want pending, real, prepared by the acting user", p)
	}
	var out struct {
		Status       string `json:"status"`
		WithdrawalID string `json:"withdrawal_id"`
	}
	for _, b := range run.ToolResults {
		if b.ToolName == agent.ToolPrepareTrustWithdrawal && !b.IsError {
			if err := json.Unmarshal([]byte(b.Text), &out); err != nil {
				t.Fatalf("tool result: %v", err)
			}
		}
	}
	w, err := fd.Withdrawals.Get(ctx, out.WithdrawalID)
	if err != nil {
		t.Fatalf("withdrawal %q: %v", out.WithdrawalID, err)
	}
	r := w.Request
	// rule: ARC-U-027
	if out.Status != "proposed" || w.Status != withdraw.StatusRequested || w.GateID != p.GateID || r.AmountCents != bilcTrust ||
		r.BillID != bilcBill || r.MatterID != agenttest.TrustMatter || r.PreparedBy != agenttest.FeatureActingFor ||
		// rule: BIL-L-037
		r.Payee != agenttest.FirmPayee {
		t.Fatalf("withdrawal = %+v (%s), want requested %d for bill %s to the firm, prepared by the acting user", w, out.Status, bilcTrust, bilcBill)
	}
	// rule: BIL-L-035
	// A withdrawal that pays a bill opens the Def's gate kind (G-B4) in the trust module.
	if got := withdraw.RequestGateKind(r); got != billing.GateTrustFeeWithdrawal || !slices.Contains(billing.TrustApplicationDef().GateKinds, got) {
		t.Fatalf("gate kind = %s, want %s", got, billing.GateTrustFeeWithdrawal)
	}
	if n, err := fd.Withdrawals.Available(ctx, agenttest.TrustAccount, agenttest.TrustMatter); err != nil || n != bilcTrust {
		t.Fatalf("trust after the proposal = %d (%v), want %d untouched", n, err, bilcTrust)
	}
	bilcDeciders(t, gates.Kind{Code: billing.GateTrustFeeWithdrawal, SpecGate: "G-B4", Approver: "trust.authorize"}, agenttest.FeatureActingFor, p.GateID)
	if _, err := fd.Decided(ctx, agent.GateDecided{GateID: p.GateID, Decision: agent.Approved, By: agenttest.FeatureActingFor}); !errors.Is(err, agent.ErrSelfApproval) {
		t.Fatalf("acting user decided own gate: err %v, want ErrSelfApproval", err)
	}
}

// rule: ARC-U-027
// The feature only prepares: its source reaches no module API that authorizes, approves,
// delivers or executes.
func TestFBIL03_PrepareOnly(t *testing.T) {
	if err := bilcPrepareOnly("ai_f_bil_03.go"); err != nil {
		t.Fatal(err)
	}
}

// rule: AI-R-002
func TestFBIL03_Def(t *testing.T) {
	def := billing.TrustApplicationDef()
	bilcPromptLint(t, def, "Trust application proposal", "transfer proposal for G-B4")
	b, err := billing.TrustApplicationBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	tests := []struct {
		field     string
		got, want string
	}{
		{"feature", b.FeatureID, "F-BIL-03"},
		{"events", strings.Join(b.Events, ","), billing.EventBillDelivered},
		{"cadence", string(b.Cadence), string(trigger.CadenceNone)},
		{"acting for", b.ActingFor, ""},
		{"trigger", def.Trigger, "event: bill delivered and retainer permits"},
		{"on request", strings.Join(def.Actions, ";"), "trust-to-fees > Propose transfer"},
		{"gate kinds", strings.Join(def.GateKinds, ","), "billing.trust_fee_withdrawal"},
		{"source", string(def.Source), "job"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}
