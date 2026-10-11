package trust_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// Shared test helpers of the trust features that prepare money movement with
// prepare_trust_withdrawal (F-TRU-08, F-TRU-10), prefix trud. Their runs go through the
// app's production handlers only: every Def tool, prepare_trust_withdrawal included, is built
// from agent.HandlerFor over the driver's trust module, and the write runs behind
// trust.SourcedCents. Every figure is synthetic and in integer cents.

const (
	// trudBill is the delivered synthetic bill on agenttest.TrustMatter.
	trudBill = "00000000-0000-4000-8000-0000000000c8"
	// trudBillTotal is trudBill's total and unpaid balance.
	trudBillTotal = 113_000
	// trudTrust is agenttest.TrustMatter's cleared trust balance in these runs.
	trudTrust = 56_500
	// trudOverdrawn is the signed balance of an overdrawn ledger.
	trudOverdrawn = -5_000
	// trudOtherLawyer holds the trust approver capability and prepared nothing.
	trudOtherLawyer = "user-synthetic-trust-partner"
)

// trudDecideAt is the decision clock: a weekday morning in America/Toronto.
var trudDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// trudPorts serves the trust and billing read tools from the driver's own trust module (the
// one the production prepare_trust_withdrawal checks) and the Seed's bills.
type trudPorts struct{ fd *agenttest.FeatureDriver }

// Get implements agent.BillReader.
func (p trudPorts) Get(ctx context.Context, id bill.ID) (bill.Bill, error) {
	return p.fd.Seed.Bills.Get(ctx, id)
}

// BalanceCents implements agent.BillBalances: the bill's stored unpaid balance.
func (p trudPorts) BalanceCents(ctx context.Context, id bill.ID) (int64, error) {
	b, err := p.fd.Seed.Bills.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	return b.BalanceCents, nil
}

// Available implements agent.TrustAvailable over the driver's trust module.
func (p trudPorts) Available(ctx context.Context, account, matterID string) (int64, error) {
	return p.fd.Withdrawals.Available(ctx, account, matterID)
}

// TrustAccountsOf implements agent.MatterTrustAccounts: every synthetic ledger is in
// agenttest.TrustAccount.
func (p trudPorts) TrustAccountsOf(context.Context, string) ([]string, error) {
	return []string{agenttest.TrustAccount}, nil
}

// trudDep appends d's value of type T to vals, when d has one.
func trudDep[T any](vals []any, d agent.Deps) []any {
	if v, err := agent.DepOf[T](d); err == nil {
		return append(vals, v)
	}
	return vals
}

// trudFeature builds def's FeatureDriver and replaces its Registry with one in which every
// Def tool is the app's production handler: the read tools record their figures in the
// driver's Provenance, and prepare_trust_withdrawal is agent.PrepareTrustWithdrawalHandler
// over the driver's WriteTools, wrapped in trust.SourcedCents. It seeds the delivered bill
// trudBill (its line narrative is narrative, the injection carrier) and a cleared trust
// balance of balance on agenttest.TrustMatter.
func trudFeature(t *testing.T, def feature.Def, narrative string, balance int64) *agenttest.FeatureDriver {
	t.Helper()
	fd := agenttest.FromFeature(t, def)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == agenttest.FeatureActingFor && slices.Contains(def.Grant, capability), nil
	})
	seed := fd.Seed.Deps(can, fd.Plans)
	ports := trudPorts{fd}
	// DepOf takes the first value of a type: the driver's Provenance (the one WriteTools
	// checks) comes ahead of the Seed's.
	vals := []any{fd.Provenance, fd.Write, agent.BillReader(ports), agent.BillBalances(ports),
		agent.TrustAvailable(ports), agent.MatterTrustAccounts(ports)}
	vals = trudDep[agent.CapabilityCheck](vals, seed)
	vals = trudDep[roles.Walls](vals, seed)
	vals = trudDep[agenttools.Plans](vals, seed)
	vals = trudDep[agenttools.GateOpener](vals, seed)
	vals = trudDep[agenttools.RunScopes](vals, seed)
	vals = trudDep[agent.SubjectMatters](vals, seed)
	vals = trudDep[matter.Query](vals, seed)
	deps := agent.NewDeps(vals...)
	reg := agenttools.NewRegistry(fd.Plans)
	for _, name := range def.Tools {
		h, ok := agent.HandlerFor(name)
		if !ok {
			t.Fatalf("%s has no production handler", name)
		}
		tool, err := h.Build(deps)
		if err != nil {
			t.Fatalf("build %s: %v", name, err)
		}
		if name == agent.ToolPrepareTrustWithdrawal {
			// rule: ARC-U-027
			// Every amount the write states is a figure this run read, in integer cents.
			if tool, err = trust.SourcedCents(fd.Provenance, tool); err != nil {
				t.Fatalf("sourced %s: %v", name, err)
			}
		}
		if err := reg.Register(tool); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	fd.Registry = reg
	fd.Real, fd.StandIns = slices.Sorted(slices.Values(def.Tools)), nil
	delivered := time.Date(2026, 9, 30, 10, 0, 0, 0, trudDecideAt.Location())
	if err := fd.Seed.Bills.Put(bill.Bill{
		ID: trudBill, Number: "SYN-2026-0008", MatterID: agenttest.TrustMatter, Status: bill.StatusDelivered,
		PreparedBy: trudOtherLawyer, FeesCents: 100_000, TaxCents: 13_000, TotalCents: trudBillTotal,
		BalanceCents: trudBillTotal, IssueDate: delivered, DeliveredAt: delivered, DeliveredOn: delivered, CreatedAt: delivered,
		Lines: []bill.Line{{Ordinal: 1, Source: bill.SourceTime, Narrative: narrative, QuantityMilli: 2_000, RateCents: 50_000, AmountCents: 100_000}},
	}); err != nil {
		t.Fatalf("seed bill: %v", err)
	}
	fd.Withdrawals.SetBalance(agenttest.TrustAccount, agenttest.TrustMatter, balance, 0)
	return fd
}

// trudWithdraw is a prepare_trust_withdrawal call on agenttest.TrustMatter.
func trudWithdraw(cents int64, purpose, payee string) truCall {
	return truCall{agent.ToolPrepareTrustWithdrawal, map[string]any{
		"matter_id": agenttest.TrustMatter, "amount_cents": cents, "purpose_category": purpose, "payee": payee,
	}}
}

// trudCalls is a script over agenttest.TrustMatter: plan, the reads, then write.
func trudCalls(write truCall) []truCall {
	return []truCall{
		truPlan("read", "propose"),
		{agent.ToolGetTrustBalance, map[string]any{"matter_id": agenttest.TrustMatter}},
		{agent.ToolGetBill, map[string]any{"bill_id": trudBill}},
		write,
	}
}

// trudSpecSync is truSpecSync for a screen-action Def: its one action is its trigger's, and
// features.json lists on_request for jobs only.
func trudSpecSync(t *testing.T, d feature.Def) {
	t.Helper()
	sf := truSpec(t, d.ID)
	if want := []string{strings.TrimPrefix(sf.Trigger, "on request: ")}; d.Source != feature.SourceScreenAction || !slices.Equal(d.Actions, want) {
		t.Fatalf("source %s actions %v, want screen-action %v", d.Source, d.Actions, want)
	}
	d.Actions = nil
	truSpecSync(t, d, false)
}

// trudWithdrawal returns the withdrawal the run's one prepare_trust_withdrawal recorded.
func trudWithdrawal(t *testing.T, fd *agenttest.FeatureDriver, run agenttest.Run) withdraw.Withdrawal {
	t.Helper()
	for _, b := range run.ToolResults {
		var out struct {
			Status       string `json:"status"`
			WithdrawalID string `json:"withdrawal_id"`
		}
		if b.ToolName != agent.ToolPrepareTrustWithdrawal || b.IsError || json.Unmarshal([]byte(b.Text), &out) != nil {
			continue
		}
		w, err := fd.Withdrawals.Get(context.Background(), out.WithdrawalID)
		if err != nil || out.Status != "proposed" {
			t.Fatalf("withdrawal %q (%s): %v", out.WithdrawalID, out.Status, err)
		}
		return w
	}
	t.Fatal("no withdrawal recorded")
	return withdraw.Withdrawal{}
}

// trudNoWrite fails t when the run left a withdrawal row, a gate or a changed balance.
func trudNoWrite(t *testing.T, fd *agenttest.FeatureDriver, run agenttest.Run, balance int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := fd.Withdrawals.Get(ctx, "wd_1"); err == nil || len(run.GateIDs) != 0 {
		t.Fatalf("refused write left a withdrawal row or gates %v", run.GateIDs)
	}
	if n, err := fd.Withdrawals.Available(ctx, agenttest.TrustAccount, agenttest.TrustMatter); err != nil || n != balance {
		t.Fatalf("trust balance = %d (%v), want %d untouched", n, err, balance)
	}
}

// trudCatalog is the production gate catalog over app/legal/gates/kinds.json, with the
// trust kinds' cooling-off periods.
func trudCatalog(t *testing.T) *gates.Catalog {
	t.Helper()
	b, err := os.ReadFile("../gates/kinds.json")
	if err != nil {
		t.Fatal(err)
	}
	kinds, err := gates.ParseKinds(b)
	if err != nil {
		t.Fatal(err)
	}
	return gates.NewCatalog(gates.WithCoolingOff(kinds))
}

// trudDeciders checks, with the production gate catalog, who may decide a gate of kind the
// agent prepared for the acting user: never an agent principal (403), never the acting user
// (403), only another approver once any cooling-off has run.
func trudDeciders(t *testing.T, kind, gateID string) {
	t.Helper()
	ctx := context.Background()
	cat := trudCatalog(t)
	k, err := cat.Kind(ctx, kind)
	if err != nil {
		t.Fatalf("kind %s: %v", kind, err)
	}
	g := gates.Gate{ID: gateID, Code: kind, OpenedAt: trudDecideAt,
		Preparer: gates.Principal{UserID: "agent", Agent: true, ActingFor: agenttest.FeatureActingFor}}
	approver := []string{k.Approver}
	after := trudDecideAt.Add(k.CoolingOff + time.Hour)
	tests := []struct {
		name    string
		by      gates.Principal
		wantErr error
		status  int
	}{
		// rule: ARC-I-039
		{"agent principal", gates.Principal{UserID: "agent", Agent: true, ActingFor: trudOtherLawyer, Capabilities: approver}, gates.ErrAgentApproves, 403},
		{"acting user decides own gate", gates.Principal{UserID: agenttest.FeatureActingFor, Capabilities: approver}, gates.ErrPreparerApproves, 403},
		{"another lawyer", gates.Principal{UserID: trudOtherLawyer, Capabilities: approver}, nil, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cat.CheckDecision(ctx, g, tt.by, after)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) || agenttest.DecisionStatus(err) != tt.status {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, agenttest.DecisionStatus(err), tt.wantErr, tt.status)
			}
		})
	}
}

// trudAbsent fails t when text is in any prepare_trust_withdrawal input of run or in the
// withdrawal w it recorded (the proposal the gate's payload is built from).
func trudAbsent(t *testing.T, run agenttest.Run, w withdraw.Withdrawal, text string) {
	t.Helper()
	for _, req := range run.Requests {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Type == "tool_use" && b.ToolName == agent.ToolPrepareTrustWithdrawal && strings.Contains(string(b.Input), text) {
					t.Fatalf("%s input carries the injected text", b.ToolName)
				}
			}
		}
	}
	r := w.Request
	if strings.Contains(strings.Join([]string{r.MatterID, r.Payee, r.Reason, r.BillID, r.EFTAuthorizationID}, "\n"), text) {
		t.Fatalf("withdrawal %s carries the injected text", w.ID)
	}
}
