package boot

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/app/legal/trust/withdraw/withdrawfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	wtRun     = "run-wt"
	wtClerk   = "user-clerk"
	wtMatter  = "00000000-0000-0000-0000-00000000000a"
	wtAccount = "acct-1"
	wtPayee   = "00000000-0000-0000-0000-0000000000c1"
	wtFirm    = "00000000-0000-0000-0000-0000000000f1"
)

type wtContacts map[string][]string

func (c wtContacts) MatterContactIDs(_ context.Context, m string) ([]string, error) { return c[m], nil }

type wtAccounts map[string][]string

func (a wtAccounts) TrustAccountsOf(_ context.Context, m string) ([]string, error) { return a[m], nil }

type wtBills struct{}

func (wtBills) FeeBill(context.Context, string, string) (withdraw.FeeBill, error) {
	return withdraw.FeeBill{}, errors.New("no bill")
}

// wtWorld is boot's write-tool wiring over fakes: run wtRun read the figure 5000 and the
// matter's one trust ledger holds 500000.
func wtWorld(t *testing.T) (AgentPorts, agent.WriteDeps, *withdrawfake.Fake) {
	t.Helper()
	wf := withdrawfake.New(24 * time.Hour)
	wf.SetBalance(wtAccount, wtMatter, 500_000, 0)
	// rule: ARC-U-027
	// The agent path never authorizes or executes: the fake fails the test if it does.
	wf.GuardEffects(t)
	pv := agenttools.NewProvenance()
	pv.Observe(wtRun, json.RawMessage(`{"balance_cents":5000}`))
	p := AgentPorts{
		Can:            func(_ context.Context, user, capability string) (bool, error) { return user == wtClerk, nil },
		Walls:          rolesfake.New(),
		Provenance:     pv,
		MatterContacts: wtContacts{wtMatter: {wtPayee}},
		TrustAccounts:  wtAccounts{wtMatter: {wtAccount}},
	}
	w := agent.WriteDeps{Withdrawals: wf, FirmAccount: firmAccount(wtFirm), Bills: wtBills{}}
	return p, w, wf
}

// rule: ARC-I-035
// rule: ARC-U-027
// Boot wires prepare_trust_withdrawal to the real write deps: a run whose feature declares
// trust.withdrawal records a requested withdrawal with its gate pending; an undeclared kind
// and a nil port are refused and record nothing.
func TestAgentWriteToolsPrepareTrustWithdrawal(t *testing.T) {
	in := json.RawMessage(`{"matter_id":"` + wtMatter + `","amount_cents":5000,"purpose_category":"disbursement","payee":"` + wtPayee + `"}`)
	tests := []struct {
		name    string
		kinds   []string
		edit    func(*AgentPorts, *agent.WriteDeps)
		wantErr error
	}{
		{"declared kind and valid data: requested, gate pending", []string{agent.GateTrustWithdrawal}, nil, nil},
		{"undeclared kind: refused", []string{"unrelated.kind"}, nil, agenttools.ErrGateKindNotAllowed},
		{"no kinds: refused", nil, nil, agenttools.ErrGateKindNotAllowed},
		{"nil withdrawals: refused", []string{agent.GateTrustWithdrawal}, func(_ *AgentPorts, w *agent.WriteDeps) { w.Withdrawals = nil }, agent.ErrMissingDep},
		{"nil provenance: refused", []string{agent.GateTrustWithdrawal}, func(p *AgentPorts, _ *agent.WriteDeps) { p.Provenance = nil }, agent.ErrMissingDep},
		{"nil contacts: refused", []string{agent.GateTrustWithdrawal}, func(p *AgentPorts, _ *agent.WriteDeps) { p.MatterContacts = nil }, agent.ErrMissingDep},
		{"nil firm account: refused", []string{agent.GateTrustWithdrawal}, func(_ *AgentPorts, w *agent.WriteDeps) { w.FirmAccount = nil }, agent.ErrMissingDep},
		{"nil bills: refused", []string{agent.GateTrustWithdrawal}, func(_ *AgentPorts, w *agent.WriteDeps) { w.Bills = nil }, agent.ErrMissingDep},
		{"nil trust accounts: refused", []string{agent.GateTrustWithdrawal}, func(p *AgentPorts, _ *agent.WriteDeps) { p.TrustAccounts = nil }, agent.ErrMissingDep},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, w, wf := wtWorld(t)
			if tc.edit != nil {
				tc.edit(&p, &w)
			}
			wt, err := AgentWriteTools(p, w)
			if err != nil {
				// A nil port fails boot closed: no write tool is served.
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("build err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			tool, err := agent.PrepareTrustWithdrawalHandler().Build(agent.NewDeps(wt))
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			plans := agenttoolsfake.New()
			if _, err := plans.ReportPlan(context.Background(), wtRun, []string{"prepare the transfer"}); err != nil {
				t.Fatalf("plan: %v", err)
			}
			reg := agenttools.NewRegistry(plans)
			if err := reg.Register(tool); err != nil {
				t.Fatalf("register: %v", err)
			}
			ctx := agenttools.WithScope(context.Background(), agenttools.NewScope(wtRun, tc.kinds))
			agentP := registry.Principal{Kind: registry.PrincipalAgent, RunID: wtRun, ActingFor: wtClerk, Grant: []string{agent.CapTrustRequest}}
			res, err := reg.Call(ctx, agentP, agent.ToolPrepareTrustWithdrawal, in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			got, gerr := wf.Get(context.Background(), "wd_1")
			if tc.wantErr != nil {
				if gerr == nil {
					t.Fatalf("refused call recorded withdrawal %+v, want none", got)
				}
				return
			}
			if gerr != nil {
				t.Fatalf("withdrawal: %v", gerr)
			}
			// rule: ARC-I-045
			// Recorded as requested by the acting user, with its Counsel gate pending.
			if got.Status != withdraw.StatusRequested || got.GateID == "" || res.GateID != got.GateID || got.Request.PreparedBy != wtClerk || got.AuthorizedBy != "" {
				t.Fatalf("withdrawal = %+v, gate %q; want requested by %s with its gate pending", got, res.GateID, wtClerk)
			}
		})
	}
}

// rule: ARC-U-027
// The trust module reaches the agent as Request and Available only: the boot wrapper hides
// Authorize and RecordExecution even when the module behind it has them.
func TestRequestOnlyHidesAuthorize(t *testing.T) {
	var w agent.Withdrawals = requestOnly{w: withdrawfake.New(time.Hour)}
	if _, ok := w.(interface {
		Authorize(context.Context, string, string, time.Time) (withdraw.Withdrawal, error)
	}); ok {
		t.Fatal("requestOnly exposes Authorize")
	}
	if _, ok := w.(interface {
		RecordExecution(context.Context, string, string, time.Time) (withdraw.Withdrawal, error)
	}); ok {
		t.Fatal("requestOnly exposes RecordExecution")
	}
}

// A matter whose trust money sits in no ledger, or in several, gets no trust account: the
// agent never picks the account a withdrawal leaves from.
func TestSoleTrustAccount(t *testing.T) {
	tests := []struct {
		name    string
		accts   []string
		want    string
		wantErr error
	}{
		{"one ledger", []string{"a"}, "a", nil},
		// rule: BIL-L-036
		{"no ledger", nil, "", agent.ErrInvalidInput},
		{"several ledgers", []string{"a", "b"}, "", agent.ErrInvalidInput},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := soleTrustAccount(wtAccounts{wtMatter: tc.accts})(context.Background(), wtMatter)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %q, %v; want %q, %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

// rule: ARC-U-027
// Write tools built with empty deps (the boot default before this wiring) refuse every call
// at call time instead of panicking, even in a run that declares trust.withdrawal.
func TestEmptyWriteToolsRefuse(t *testing.T) {
	wt := mustWriteTools()
	tool, err := agent.PrepareTrustWithdrawalHandler().Build(agent.NewDeps(wt))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	ctx := agenttools.WithScope(context.Background(), agenttools.NewScope(wtRun, []string{agent.GateTrustWithdrawal}))
	p := registry.Principal{Kind: registry.PrincipalAgent, RunID: wtRun, ActingFor: wtClerk}
	in := json.RawMessage(`{"matter_id":"` + wtMatter + `","amount_cents":5000,"purpose_category":"disbursement","payee":"` + wtPayee + `"}`)
	if _, err := tool.Handle(ctx, p, in); !errors.Is(err, agent.ErrMissingDep) {
		t.Fatalf("err = %v, want ErrMissingDep", err)
	}
}
