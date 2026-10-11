package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/app/legal/trust/withdraw/withdrawfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	xferRun = "run-xfer"
	// xferFrom holds 50000 cents in xferAccount; xferTo is a second matter in that account.
	xferFrom    = "a2222222-2222-4222-8222-222222222201"
	xferTo      = "a2222222-2222-4222-8222-222222222202"
	xferWalled  = "a2222222-2222-4222-8222-222222222203"
	xferElse    = "a2222222-2222-4222-8222-222222222204" // a ledger in another trust account only
	xferAccount = "acct-xfer"
	xferHeld    = 50_000
)

// xferLedgers answers TrustAvailable and MatterTrustAccounts over fixed ledgers.
type xferLedgers struct{}

func (xferLedgers) Available(_ context.Context, account, matterID string) (int64, error) {
	if account == xferAccount && matterID == xferFrom {
		return xferHeld, nil
	}
	return 0, nil
}

func (xferLedgers) TrustAccountsOf(_ context.Context, matterID string) ([]string, error) {
	if matterID == xferElse {
		return []string{"acct-other"}, nil
	}
	return []string{xferAccount}, nil
}

type xferWorld struct {
	reg       *agenttools.Registry
	gates     *gatefake.Fake
	transfers *withdrawfake.Transfers
}

// newXferWorld builds prepare_trust_transfer through its handler. The run read 50000 (the
// source balance), 20000 and -5000 (an overdrawn ledger).
func newXferWorld(t *testing.T, caps map[string]bool) xferWorld {
	t.Helper()
	ctx := context.Background()
	walls := rolesfake.New()
	walls.Wall(clerk, xferWalled)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && caps[capability], nil
	})
	w := xferWorld{
		gates: gatefake.New(func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }, gatefake.Caps{clerk: {agent.CapTrustRequest}}),
	}
	if err := w.gates.Register(gate.GateKind{
		Slug: agent.GateTrustTransfer, Approver: "trust.approve", Risk: gate.RiskHigh, MinApprovers: 1, TTL: 72 * time.Hour,
		Effect: func(context.Context, pgx.Tx, gate.Approved) error { return nil },
	}); err != nil {
		t.Fatalf("gate kind: %v", err)
	}
	w.transfers = withdrawfake.NewTransfers()
	w.transfers.AddLedger(xferAccount, xferFrom, xferHeld)
	w.transfers.AddLedger(xferAccount, xferTo, 0)
	open := agent.TrustTransfers(w.transfers.Requests(w.gates))
	pv := agenttools.NewProvenance()
	pv.Observe(xferRun, json.RawMessage(`{"balance_cents":50000,"part_cents":20000,"overdrawn_cents":-5000,"big_cents":60000}`))
	ledgers := xferLedgers{}
	tool, err := agent.PrepareTrustTransferHandler().Build(agent.NewDeps(can, walls, agent.TrustAvailable(ledgers),
		agent.MatterTrustAccounts(ledgers), open, pv))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, xferRun, []string{"prepare the transfer"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	w.reg = agenttools.NewRegistry(plans)
	if err := w.reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return w
}

func xferInput(from, to string, cents int64) string {
	b, _ := json.Marshal(map[string]any{"from_matter_id": from, "to_matter_id": to, "amount_cents": cents, "reason": "matter split"})
	return string(b)
}

func xferPrincipal() registry.Principal {
	return registry.Principal{Kind: registry.PrincipalAgent, RunID: xferRun, ActingFor: clerk, Grant: []string{agent.CapTrustRequest}}
}

func xferScope(kinds ...string) context.Context {
	return agenttools.WithScope(context.Background(), agenttools.NewScope(xferRun, kinds))
}

// rule: ARC-I-025
// rule: ARC-I-035
// rule: ARC-I-036
// rule: ARC-I-039
// rule: ARC-U-027
// prepare_trust_transfer opens one trust.transfer gate prepared by the acting user, and
// refuses an invented amount, an overdraw, a walled or unshared matter and a run without the
// gate kind before any gate exists.
func TestPrepareTrustTransfer(t *testing.T) {
	tests := []struct {
		name    string
		caps    map[string]bool
		kinds   []string
		in      string
		wantErr error
	}{
		{"whole balance moves", map[string]bool{agent.CapTrustRequest: true}, []string{agent.GateTrustTransfer}, xferInput(xferFrom, xferTo, 50_000), nil},
		{"part of the balance moves", map[string]bool{agent.CapTrustRequest: true}, []string{agent.GateTrustTransfer}, xferInput(xferFrom, xferTo, 20_000), nil},
		// rule: ARC-U-027
		{"invented amount", map[string]bool{agent.CapTrustRequest: true}, []string{agent.GateTrustTransfer}, xferInput(xferFrom, xferTo, 12_345), agenttools.ErrUnsourcedValue},
		// rule: ARC-I-025
		{"overdraw of the source ledger", map[string]bool{agent.CapTrustRequest: true}, []string{agent.GateTrustTransfer}, xferInput(xferFrom, xferTo, 60_000), withdraw.ErrTrustOverdraw},
		{"unsigned overdraft", map[string]bool{agent.CapTrustRequest: true}, []string{agent.GateTrustTransfer}, xferInput(xferFrom, xferTo, 5_000), agenttools.ErrUnsourcedValue},
		{"negative amount", map[string]bool{agent.CapTrustRequest: true}, []string{agent.GateTrustTransfer}, xferInput(xferFrom, xferTo, -5_000), agenttools.ErrInvalidInput},
		// rule: ARC-I-036
		{"walled source", map[string]bool{agent.CapTrustRequest: true}, []string{agent.GateTrustTransfer}, xferInput(xferWalled, xferTo, 20_000), agent.ErrNotVisible},
		{"walled destination", map[string]bool{agent.CapTrustRequest: true}, []string{agent.GateTrustTransfer}, xferInput(xferFrom, xferWalled, 20_000), agent.ErrNotVisible},
		{"same matter", map[string]bool{agent.CapTrustRequest: true}, []string{agent.GateTrustTransfer}, xferInput(xferFrom, xferFrom, 20_000), agent.ErrInvalidInput},
		{"no shared trust account", map[string]bool{agent.CapTrustRequest: true}, []string{agent.GateTrustTransfer}, xferInput(xferFrom, xferElse, 20_000), agent.ErrTransferAccounts},
		{"acting user lacks trust.request", map[string]bool{}, []string{agent.GateTrustTransfer}, xferInput(xferFrom, xferTo, 20_000), agent.ErrForbidden},
		// rule: ARC-I-035
		{"run scope lacks trust.transfer", map[string]bool{agent.CapTrustRequest: true}, []string{agent.GateTrustWithdrawal}, xferInput(xferFrom, xferTo, 20_000), agenttools.ErrGateKindNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newXferWorld(t, tt.caps)
			res, err := w.reg.Call(xferScope(tt.kinds...), xferPrincipal(), agent.ToolPrepareTrustTransfer, json.RawMessage(tt.in))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if len(w.transfers.Rows()) != 0 || gatesOpened(w.gates) != 0 || res.GateID != "" {
					t.Fatalf("refused call recorded %d transfers", len(w.transfers.Rows()))
				}
				return
			}
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			g, err := w.gates.Get(context.Background(), gate.GateID(res.GateID))
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			// rule: ARC-I-039
			// One pending trust.transfer gate; the acting user is its preparer, the agent
			// never decides it.
			if gatesOpened(w.gates) != 1 || g.Kind != agent.GateTrustTransfer || g.Status != gate.StatusPending ||
				g.Preparer.ActingFor != clerk || g.Preparer.Kind != registry.PrincipalAgent {
				t.Fatalf("gate = %+v, want one pending %s prepared for %s", g, agent.GateTrustTransfer, clerk)
			}
			// rule: ARC-I-027
			// The transfer row is pending authorization, prepared by the acting user, and the
			// gate's payload names it: approval moves exactly this row.
			rows := w.transfers.Rows()
			var p withdraw.TransferPayload
			if err := json.Unmarshal(g.Payload, &p); err != nil || p.FromMatterID != xferFrom || p.ToMatterID != xferTo || p.AmountCents == nil {
				t.Fatalf("payload = %s (%v)", g.Payload, err)
			}
			if len(rows) != 1 || rows[0].Status != "pending_authorization" || rows[0].RequestedBy != clerk ||
				rows[0].GateID != res.GateID || p.TransferID != rows[0].ID || rows[0].AmountCents != *p.AmountCents {
				t.Fatalf("rows = %+v, payload = %+v", rows, p)
			}
		})
	}
}

// rule: ARC-U-027
// A missing port fails the build: the tool never runs with a check skipped.
func TestPrepareTrustTransferNeedsEveryPort(t *testing.T) {
	ledgers := xferLedgers{}
	can := agent.CapabilityCheck(func(context.Context, string, string) (bool, error) { return true, nil })
	open := agent.TrustTransfers(withdrawfake.NewTransfers().Requests(nil))
	// Each port is its own value, so leaving one out leaves nothing else that serves it.
	avail := struct{ agent.TrustAvailable }{ledgers}
	accounts := struct{ agent.MatterTrustAccounts }{ledgers}
	full := []any{can, rolesfake.New(), avail, accounts, open, agenttools.NewProvenance()}
	for i := range full {
		t.Run("without dep "+string(rune('0'+i)), func(t *testing.T) {
			deps := append(append([]any{}, full[:i]...), full[i+1:]...)
			if _, err := agent.PrepareTrustTransferHandler().Build(agent.NewDeps(deps...)); !errors.Is(err, agent.ErrMissingDep) {
				t.Fatalf("build = %v, want ErrMissingDep", err)
			}
		})
	}
}
