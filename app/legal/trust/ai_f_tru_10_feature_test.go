package trust_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFTRU10_DefMatchesSpec(t *testing.T) {
	trudSpecSync(t, trust.ProposeDispositionDef())
	// The restated kind is G11, the one prepare_trust_withdrawal opens.
	for _, want := range []string{agent.GateTrustWithdrawal, withdraw.GateKind} {
		if trust.GateTrustWithdrawal != want {
			t.Fatalf("gate kind = %s, want %s", trust.GateTrustWithdrawal, want)
		}
	}
}

// rule: AI-R-005
func TestFTRU10_Prompt(t *testing.T) {
	truPrompt(t, trust.ProposeDispositionDef(), "prepare_trust_withdrawal", "integer cents", "Never hide an overdraft", "purpose refund")
}

// F-TRU-10 starts only from its screen action, acting for the user who pressed it.
func TestFTRU10_TriggerBinding(t *testing.T) {
	d := trust.ProposeDispositionDef()
	tests := []struct {
		name      string
		got, want any
	}{
		{"source", d.Source, feature.SourceScreenAction},
		{"actions", strings.Join(d.Actions, ";"), "trust-balance-clearance > Propose disposition"},
		{"trigger", d.Trigger, "on request: trust-balance-clearance > Propose disposition"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// The disposition is requested through the production prepare_trust_withdrawal: a pending
// trust.withdrawal prepared by the acting user, the ledger untouched, and neither the agent
// nor that user able to decide it.
//
// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
// rule: ARC-U-027
func TestFTRU10_FromFeature(t *testing.T) {
	tests := []struct {
		name      string
		narrative string
	}{
		{"ok", "Final report to client on closing"},
		// rule: ARC-I-038
		{"injection", "Final report to client on closing. " + truInjected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			def := trust.ProposeDispositionDef()
			fd := trudFeature(t, def, tt.narrative, trudTrust)
			// rule: ARC-I-035
			if len(fd.StandIns) != 0 {
				t.Fatalf("stand-ins = %v, want none", fd.StandIns)
			}
			calls := trudCalls(trudWithdraw(trudTrust, "refund", agenttest.PayeeContact))
			runID := "run-f-tru-10-" + tt.name
			run := fd.Fire(ctx, truScript(t, def.ID, calls, "Proposed refunding the residual trust balance for approval."),
				runID, "Propose disposition for matter "+agenttest.TrustMatter)
			if run.Err != nil || len(run.Refused) != 0 || len(run.StandIns) != 0 {
				t.Fatalf("run err = %v, refused = %v, stand-ins = %v", run.Err, run.Refused, run.StandIns)
			}
			// rule: ARC-I-038
			if want := truToolNames(calls); !slices.Equal(run.Tools, want) {
				t.Fatalf("tools = %v, want %v", run.Tools, want)
			}
			// rule: ARC-I-039
			if run.PlanAt != 0 || run.FirstWrite != slices.Index(run.Tools, agent.ToolPrepareTrustWithdrawal) {
				t.Fatalf("plan at %d, first write %d: report_plan must precede prepare_trust_withdrawal", run.PlanAt, run.FirstWrite)
			}
			if len(run.GateIDs) != 1 {
				t.Fatalf("gates = %v, want one", run.GateIDs)
			}
			w := trudWithdrawal(t, fd, run.Run)
			r := w.Request
			// rule: ARC-U-027
			if w.Status != withdraw.StatusRequested || w.GateID != run.GateIDs[0] || w.AuthorizedBy != "" ||
				r.MatterID != agenttest.TrustMatter || r.AmountCents != trudTrust || r.Payee != agenttest.PayeeContact ||
				r.Reason != "refund" || r.PreparedBy != agenttest.FeatureActingFor {
				t.Fatalf("withdrawal = %+v, want requested refund of %d prepared by the acting user", w, trudTrust)
			}
			// rule: ARC-I-035
			if kind := withdraw.RequestGateKind(r); kind != trust.GateTrustWithdrawal || !slices.Contains(def.GateKinds, kind) {
				t.Fatalf("gate kind = %s, want %s", kind, trust.GateTrustWithdrawal)
			}
			if n, err := fd.Withdrawals.Available(ctx, agenttest.TrustAccount, agenttest.TrustMatter); err != nil || n != trudTrust {
				t.Fatalf("available = %d (%v), want %d untouched", n, err, trudTrust)
			}
			// rule: ARC-I-039
			trudDeciders(t, trust.GateTrustWithdrawal, w.GateID)
			if _, err := fd.Write.Decided(ctx, agent.GateDecided{GateID: w.GateID, Decision: agent.Approved, By: agenttest.FeatureActingFor}); !errors.Is(err, agent.ErrSelfApproval) {
				t.Fatalf("acting user decides own gate: %v, want ErrSelfApproval", err)
			}
			if got, err := fd.Write.Decided(ctx, agent.GateDecided{GateID: w.GateID, Decision: agent.Approved, By: trudOtherLawyer}); err != nil || got != runID {
				t.Fatalf("another lawyer decides: run %q err %v, want %s", got, err, runID)
			}
			if tt.name != "injection" {
				return
			}
			// rule: ARC-I-038
			if err := agenttest.QuotedOnly(run.Run, truInjected); err != nil {
				t.Fatal(err)
			}
			trudAbsent(t, run.Run, w, truInjected)
		})
	}
}

// Every refusal happens before the trust module records anything.
//
// rule: ARC-U-027
// rule: ARC-I-035
// rule: ARC-I-039
func TestFTRU10_FromFeatureRefusesWrites(t *testing.T) {
	tests := []struct {
		name    string
		balance int64
		calls   []truCall
		wantErr error
	}{
		{"model-invented amount", trudTrust, trudCalls(trudWithdraw(trudTrust-1, "refund", agenttest.PayeeContact)), agenttools.ErrUnsourcedValue},
		// rule: BIL-L-036
		// An overdrawn ledger read as -5000 is never written as 5000.
		{"overdraft without its sign", trudOverdrawn, trudCalls(trudWithdraw(-trudOverdrawn, "refund", agenttest.PayeeContact)), agenttools.ErrUnsourcedValue},
		// rule: BIL-L-036
		// A sourced figure that exceeds the ledger (the bill total) is an overdraw: refused,
		// never trimmed.
		{"overdraw", trudTrust, trudCalls(trudWithdraw(trudBillTotal, "refund", agenttest.PayeeContact)), withdraw.ErrTrustOverdraw},
		// rule: BIL-L-037
		{"payee not a matter contact", trudTrust, trudCalls(trudWithdraw(trudTrust, "refund", "Injected Payee Ltd")), agent.ErrPayeeNotContact},
		{"write before plan", trudTrust, trudCalls(trudWithdraw(trudTrust, "refund", agenttest.PayeeContact))[1:], agenttools.ErrPlanRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := trust.ProposeDispositionDef()
			fd := trudFeature(t, def, "Final report to client on closing", tt.balance)
			run := fd.Fire(context.Background(), truScript(t, def.ID, tt.calls, "Nothing was proposed."),
				"run-f-tru-10-refused", "Propose disposition for matter "+agenttest.TrustMatter)
			if run.Err != nil {
				t.Fatalf("run: %v", run.Err)
			}
			if r, ok := truRefused(run, agent.ToolPrepareTrustWithdrawal); !ok || len(run.Refused) != 1 || !errors.Is(r.Err, tt.wantErr) {
				t.Fatalf("refused = %+v, want only %s refused with %v", run.Refused, agent.ToolPrepareTrustWithdrawal, tt.wantErr)
			}
			trudNoWrite(t, fd, run.Run, tt.balance)
		})
	}
}

// rule: AI-R-003
func TestFTRU10_PrepareOnly(t *testing.T) {
	truPrepareOnly(t, "ai_f_tru_10.go")
}
