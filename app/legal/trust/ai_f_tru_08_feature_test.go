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
func TestFTRU08_DefMatchesSpec(t *testing.T) {
	trudSpecSync(t, trust.DraftTransferDef())
	// The restated kind is G11t, the one the trust module's transfer effect applies.
	if trust.GateTrustTransfer != withdraw.TransferGateKind {
		t.Fatalf("gate kind = %s, want %s", trust.GateTrustTransfer, withdraw.TransferGateKind)
	}
}

// rule: AI-R-005
func TestFTRU08_Prompt(t *testing.T) {
	truPrompt(t, trust.DraftTransferDef(), "prepare_trust_withdrawal", "integer cents", "Never hide an overdraft", "never try another tool")
}

// F-TRU-08 starts only from its screen action, acting for the user who pressed it.
func TestFTRU08_TriggerBinding(t *testing.T) {
	d := trust.DraftTransferDef()
	tests := []struct {
		name      string
		got, want any
	}{
		{"source", d.Source, feature.SourceScreenAction},
		{"actions", strings.Join(d.Actions, ";"), "trust-transfers > Draft transfer"},
		{"trigger", d.Trigger, "on request: trust-transfers > Draft transfer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// F-TRU-08's run may open trust.transfer only. prepare_trust_withdrawal opens
// trust.withdrawal, so through the production handler every proposal in this run is refused
// before the trust module records anything (Q-FEAT-TRU-08): a ledger-to-ledger transfer is
// never filed as money leaving the account. Provenance is checked first, so a model-invented
// or unsigned amount is refused as unsourced.
//
// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
// rule: ARC-U-027
func TestFTRU08_FromFeature(t *testing.T) {
	tests := []struct {
		name      string
		narrative string
		balance   int64
		calls     []truCall
		wantErr   error
	}{
		// rule: ARC-I-035
		{name: "sourced transfer fails closed outside trust.transfer", narrative: "Matter split review", balance: trudTrust,
			calls: trudCalls(trudWithdraw(trudTrust, "client_payment", agenttest.PayeeContact)), wantErr: agenttools.ErrGateKindNotAllowed},
		// rule: ARC-U-027
		{name: "model-invented amount", narrative: "Matter split review", balance: trudTrust,
			calls: trudCalls(trudWithdraw(trudTrust-100, "client_payment", agenttest.PayeeContact)), wantErr: agenttools.ErrUnsourcedValue},
		// rule: BIL-L-036
		// An overdrawn ledger read as -5000 is never written as 5000.
		{name: "overdraft without its sign", narrative: "Matter split review", balance: trudOverdrawn,
			calls: trudCalls(trudWithdraw(-trudOverdrawn, "client_payment", agenttest.PayeeContact)), wantErr: agenttools.ErrUnsourcedValue},
		// rule: ARC-I-039
		{name: "write before plan", narrative: "Matter split review", balance: trudTrust,
			calls: trudCalls(trudWithdraw(trudTrust, "client_payment", agenttest.PayeeContact))[1:], wantErr: agenttools.ErrPlanRequired},
		// rule: ARC-I-038
		{name: "injection", narrative: "Matter split review. " + truInjected, balance: trudTrust,
			calls: trudCalls(trudWithdraw(trudTrust, "client_payment", agenttest.PayeeContact)), wantErr: agenttools.ErrGateKindNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			def := trust.DraftTransferDef()
			fd := trudFeature(t, def, tt.narrative, tt.balance)
			// rule: ARC-I-035
			if len(fd.StandIns) != 0 {
				t.Fatalf("stand-ins = %v, want none", fd.StandIns)
			}
			model := truScript(t, def.ID, tt.calls, "The transfer could not be prepared; nothing was recorded.")
			run := fd.Fire(ctx, model, "run-f-tru-08-"+strings.ReplaceAll(tt.name, " ", "-"), "Draft transfer for matter "+agenttest.TrustMatter)
			if run.Err != nil || len(run.StandIns) != 0 {
				t.Fatalf("run err = %v, stand-ins = %v", run.Err, run.StandIns)
			}
			// rule: ARC-I-038
			// The run makes exactly the scripted calls: quoted text adds none.
			if want := truToolNames(tt.calls); !slices.Equal(run.Tools, want) {
				t.Fatalf("tools = %v, want %v", run.Tools, want)
			}
			if r, ok := truRefused(run, agent.ToolPrepareTrustWithdrawal); !ok || len(run.Refused) != 1 || !errors.Is(r.Err, tt.wantErr) {
				t.Fatalf("refused = %+v, want only %s refused with %v", run.Refused, agent.ToolPrepareTrustWithdrawal, tt.wantErr)
			}
			// rule: ARC-I-039
			if tt.wantErr != agenttools.ErrPlanRequired && run.PlanAt != 0 {
				t.Fatalf("plan at %d: report_plan must precede the first write", run.PlanAt)
			}
			// Fail closed: no withdrawal row, no gate, the ledger untouched.
			trudNoWrite(t, fd, run.Run, tt.balance)
			if tt.name != "injection" {
				return
			}
			// rule: ARC-I-038
			if err := agenttest.QuotedOnly(run.Run, truInjected); err != nil {
				t.Fatal(err)
			}
			if err := truAbsent(ctx, fd, run, truInjected); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A trust.transfer gate prepared for the acting user is decided by neither the agent (403)
// nor that user, only by another lawyer.
//
// rule: ARC-I-039
func TestFTRU08_Deciders(t *testing.T) {
	trudDeciders(t, trust.GateTrustTransfer, "00000000-0000-4000-8000-0000000000e8")
}

// rule: AI-R-003
func TestFTRU08_PrepareOnly(t *testing.T) {
	truPrepareOnly(t, "ai_f_tru_08.go")
}
