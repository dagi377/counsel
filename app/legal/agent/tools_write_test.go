package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/app/legal/trust/withdraw/withdrawfake"
	"github.com/dagi377/counsel/kernel/agenttools"
)

const (
	preparer = "user-clerk"
	lawyer   = "user-lawyer"
	matterA  = "00000000-0000-0000-0000-00000000000a"
	matterW  = "00000000-0000-0000-0000-00000000000b"
	account  = "acct-1"
	// payeeA is a contact of matterA; payeeW is a contact of matterW only.
	payeeA = "00000000-0000-0000-0000-0000000000c1"
	payeeW = "00000000-0000-0000-0000-0000000000c2"
	// firmPayee is the firm's general account; billDelivered and billDraft are bills on matterA.
	firmPayee     = "00000000-0000-0000-0000-0000000000f1"
	billDelivered = "00000000-0000-0000-0000-0000000000b1"
	billDraft     = "00000000-0000-0000-0000-0000000000b2"
)

// errBillNotOnMatter is what fakeFeeBills answers for a bill that is not on the matter.
var errBillNotOnMatter = errors.New("bill not on matter")

// fakeFeeBills holds bills by matter, then bill id.
type fakeFeeBills map[string]map[string]withdraw.FeeBill

func (f fakeFeeBills) FeeBill(_ context.Context, matterID, billID string) (withdraw.FeeBill, error) {
	b, ok := f[matterID][billID]
	if !ok {
		return withdraw.FeeBill{}, errBillNotOnMatter
	}
	return b, nil
}

// writeBills: billDelivered has 300000 unpaid; billDraft is approved but not delivered.
var writeBills = fakeFeeBills{matterA: {
	billDelivered: {DeliveredAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), BalanceDueCents: 300_000},
	billDraft:     {BalanceDueCents: 300_000},
}}

func firmAccount(context.Context) (string, error) { return firmPayee, nil }

// writeContacts are the matter contacts the write tests propose payments to.
var writeContacts = fakeMatterContacts{matterA: {payeeA}, matterW: {payeeW}}

// countingWithdrawals records every call so tests can see that the agent path only requests.
type countingWithdrawals struct {
	*withdrawfake.Fake
	requests, authorizes, executions int
	last                             withdraw.Request
}

func (c *countingWithdrawals) Request(ctx context.Context, r withdraw.Request) (withdraw.Withdrawal, error) {
	c.requests++
	c.last = r
	return c.Fake.Request(ctx, r)
}

func (c *countingWithdrawals) Authorize(ctx context.Context, id, by string, at time.Time) (withdraw.Withdrawal, error) {
	c.authorizes++
	return c.Fake.Authorize(ctx, id, by, at)
}

func (c *countingWithdrawals) RecordExecution(ctx context.Context, id, by string, on time.Time) (withdraw.Withdrawal, error) {
	c.executions++
	return c.Fake.RecordExecution(ctx, id, by, on)
}

// newWriteTools builds the write tools where run-1 read the figures 5000 and 900000.
func newWriteTools(t *testing.T) (*agent.WriteTools, *countingWithdrawals) {
	t.Helper()
	pv := agenttools.NewProvenance()
	pv.Observe("run-1", json.RawMessage(`{"fee_cents":5000,"bill_cents":900000}`))
	return newWriteToolsWith(t, pv, writeContacts)
}

// newSourcedWriteTools is newWriteTools with the run figures in pv.
func newSourcedWriteTools(t *testing.T, pv *agenttools.Provenance) (*agent.WriteTools, *countingWithdrawals) {
	t.Helper()
	return newWriteToolsWith(t, pv, writeContacts)
}

// newWriteToolsWith builds the write tools over pv and contacts; either may be nil.
func newWriteToolsWith(t *testing.T, pv *agenttools.Provenance, contacts agent.MatterContacts) (*agent.WriteTools, *countingWithdrawals) {
	t.Helper()
	return newWriteToolsDeps(t, func(d *agent.WriteDeps) { d.Provenance, d.Contacts = pv, contacts })
}

// newWriteToolsDeps builds the write tools with every dependency set, then applies edit.
func newWriteToolsDeps(t *testing.T, edit func(*agent.WriteDeps)) (*agent.WriteTools, *countingWithdrawals) {
	t.Helper()
	wf := withdrawfake.New(24 * time.Hour)
	wf.SetBalance(account, matterA, 500_000, 0)
	wd := &countingWithdrawals{Fake: wf}
	rf := rolesfake.New()
	rf.Wall(preparer, matterW)
	deps := agent.WriteDeps{
		Can:          func(_ context.Context, user, capability string) (bool, error) { return user == preparer, nil },
		Walls:        rf,
		TrustAccount: func(context.Context, string) (string, error) { return account, nil },
		Withdrawals:  wd,
		Contacts:     writeContacts,
		FirmAccount:  firmAccount,
		Bills:        writeBills,
	}
	edit(&deps)
	tools, err := agent.NewWriteTools(deps)
	if err != nil {
		t.Fatal(err)
	}
	return tools, wd
}

func withdrawalCall(run, matterID string, cents int64, method string) agent.Call {
	return payeeCall(run, matterID, cents, method, payeeA)
}

func payeeCall(run, matterID string, cents int64, method, payee string) agent.Call {
	in := map[string]any{"matter_id": matterID, "amount_cents": cents, "purpose_category": "client_payment", "payee": payee}
	if method != "" {
		in["method"] = method
	}
	b, _ := json.Marshal(in)
	return agent.Call{RunID: run, ActingFor: preparer, Tool: agent.ToolPrepareTrustWithdrawal, Input: b}
}

// rule: ARC-U-027
func TestARC_U_027_AgentWithdrawalIsProposedOnly(t *testing.T) {
	cases := []struct {
		name    string
		call    agent.Call
		wantErr error
	}{
		{"cheque proposal", withdrawalCall("run-1", matterA, 5_000, ""), nil},
		{"overdraw refused before any gate", withdrawalCall("run-1", matterA, 900_000, ""), withdraw.ErrTrustOverdraw},
		{"walled matter not visible", withdrawalCall("run-1", matterW, 5_000, ""), agent.ErrNotVisible},
		{"wire has no trust method", withdrawalCall("run-1", matterA, 5_000, "wire"), agent.ErrInvalidInput},
		{"negative amount fails schema", withdrawalCall("run-1", matterA, -1, ""), agent.ErrInvalidInput},
		{"acting user lacks capability", agent.Call{RunID: "run-1", ActingFor: lawyer, Tool: agent.ToolPrepareTrustWithdrawal, Input: withdrawalCall("run-1", matterA, 5_000, "").Input}, agent.ErrForbidden},
		{"unknown tool", agent.Call{RunID: "run-1", ActingFor: preparer, Tool: "authorize_withdrawal", Input: json.RawMessage(`{}`)}, agent.ErrUnknownTool},
		// rule: ARC-I-027
		{"no acting user, no preparer", agent.Call{RunID: "run-1", Tool: agent.ToolPrepareTrustWithdrawal, Input: withdrawalCall("run-1", matterA, 5_000, "").Input}, agent.ErrForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools, wd := newWriteTools(t)
			ctx := context.Background()
			res, err := tools.Call(ctx, tc.call)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if wd.authorizes != 0 || wd.executions != 0 {
				t.Fatalf("agent path authorized %d / executed %d withdrawals", wd.authorizes, wd.executions)
			}
			if tc.wantErr != nil {
				return
			}
			var out struct {
				Status       string `json:"status"`
				WithdrawalID string `json:"withdrawal_id"`
			}
			if err := json.Unmarshal(res.Output, &out); err != nil {
				t.Fatal(err)
			}
			w, err := wd.Get(ctx, out.WithdrawalID)
			if err != nil {
				t.Fatal(err)
			}
			if out.Status != "proposed" || w.Status != withdraw.StatusRequested || w.Request.PreparedBy != preparer {
				t.Fatalf("withdrawal %+v, output status %q: want requested, prepared by the acting user", w, out.Status)
			}
		})
	}
}

// rule: ARC-I-045
func TestARC_I_045_CounselGateIsOnlyAuthority(t *testing.T) {
	cases := []struct {
		name    string
		ev      func(gateID string) agent.GateDecided
		wantRun string
		wantErr error
	}{
		{"other user approves -> run resumes", func(g string) agent.GateDecided {
			return agent.GateDecided{GateID: g, Decision: agent.Approved, By: lawyer}
		}, "run-1", nil},
		{"rejection also resumes the run to finish it", func(g string) agent.GateDecided {
			return agent.GateDecided{GateID: g, Decision: agent.Rejected, By: lawyer}
		}, "run-1", nil},
		{"acting-for user cannot self-approve", func(g string) agent.GateDecided {
			return agent.GateDecided{GateID: g, Decision: agent.Approved, By: preparer}
		}, "", agent.ErrSelfApproval},
		{"a gate that is not Counsel's proposal does not resume", func(string) agent.GateDecided {
			return agent.GateDecided{GateID: "paploo-gate-1", Decision: agent.Approved, By: lawyer}
		}, "", agent.ErrUnknownGate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools, wd := newWriteTools(t)
			ctx := context.Background()
			res, err := tools.Call(ctx, withdrawalCall("run-1", matterA, 5_000, ""))
			if err != nil {
				t.Fatal(err)
			}
			// The only gate is the one the Counsel withdrawal request opened.
			if wd.requests != 1 || res.GateID == "" {
				t.Fatalf("requests = %d, gate = %q; want one Counsel gate", wd.requests, res.GateID)
			}
			run, err := tools.Decided(ctx, tc.ev(res.GateID))
			if !errors.Is(err, tc.wantErr) || run != tc.wantRun {
				t.Fatalf("Decided = %q, %v; want %q, %v", run, err, tc.wantRun, tc.wantErr)
			}
			if wd.authorizes != 0 {
				t.Fatal("agent resumed a run by authorizing the effect itself")
			}
		})
	}
}

func TestPrepareTrustWithdrawalRetryProposesOnce(t *testing.T) {
	tools, _ := newWriteTools(t)
	ctx := context.Background()
	c := withdrawalCall("run-1", matterA, 5_000, "")
	first, err := tools.Call(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	second, err := tools.Call(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if first.GateID != second.GateID {
		t.Fatalf("retry opened gate %q, first %q", second.GateID, first.GateID)
	}
	if _, err := tools.Decided(ctx, agent.GateDecided{GateID: first.GateID, Decision: agent.Approved, By: lawyer}); err != nil {
		t.Fatal(err)
	}
	if _, err := tools.Decided(ctx, agent.GateDecided{GateID: first.GateID, Decision: agent.Approved, By: lawyer}); !errors.Is(err, agent.ErrUnknownGate) {
		t.Fatalf("second gate.decided err = %v, want ErrUnknownGate", err)
	}
}

// rule: ARC-U-027
// rule: ARC-I-035
// The amount must be a figure a read tool returned in the same run, and it may not exceed the
// available balance; either refusal comes before the trust module records anything.
func TestARC_U_027_WithdrawalAmountProvenance(t *testing.T) {
	cases := []struct {
		name    string
		reads   map[string]string // run -> a read tool's output in that run
		call    agent.Call
		wantErr error
	}{
		{"balance figure from this run", map[string]string{"run-1": `{"available_cents":500000}`}, withdrawalCall("run-1", matterA, 500_000, ""), nil},
		{"bill figure from this run", map[string]string{"run-1": `{"bill_id":"b-1","total_cents":125000}`}, withdrawalCall("run-1", matterA, 125_000, ""), nil},
		{"invented amount", map[string]string{"run-1": `{"available_cents":500000}`}, withdrawalCall("run-1", matterA, 125_000, ""), agenttools.ErrUnsourcedValue},
		{"figure read in another run", map[string]string{"run-2": `{"total_cents":125000}`}, withdrawalCall("run-1", matterA, 125_000, ""), agenttools.ErrUnsourcedValue},
		{"nothing read", nil, withdrawalCall("run-1", matterA, 5_000, ""), agenttools.ErrUnsourcedValue},
		{"sourced amount over the available balance", map[string]string{"run-1": `{"total_cents":900000}`}, withdrawalCall("run-1", matterA, 900_000, ""), withdraw.ErrTrustOverdraw},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pv := agenttools.NewProvenance()
			for run, out := range tc.reads {
				pv.Observe(run, json.RawMessage(out))
			}
			tools, wd := newSourcedWriteTools(t, pv)
			res, err := tools.Call(context.Background(), tc.call)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				// No withdrawal row: the trust module was never asked.
				if wd.requests != 0 || res.GateID != "" {
					t.Fatalf("refused call made %d requests, gate %q", wd.requests, res.GateID)
				}
				return
			}
			if wd.requests != 1 || res.GateID == "" {
				t.Fatalf("requests = %d, gate %q; want one proposal", wd.requests, res.GateID)
			}
		})
	}
}

// rule: ARC-U-027
// rule: BIL-L-037
// The tool fails closed: without the run's figures or the matter's contacts it refuses, and
// a payee that is not one of the matter's contact ids is refused. No refusal records anything.
func TestARC_U_027_WithdrawalFailsClosed(t *testing.T) {
	sourced := func() *agenttools.Provenance {
		pv := agenttools.NewProvenance()
		pv.Observe("run-1", json.RawMessage(`{"fee_cents":5000}`))
		return pv
	}
	cases := []struct {
		name     string
		pv       *agenttools.Provenance
		contacts agent.MatterContacts
		call     agent.Call
		wantErr  error
	}{
		{"contact payee with a sourced amount", sourced(), writeContacts, withdrawalCall("run-1", matterA, 5_000, ""), nil},
		{"nil provenance refused", nil, writeContacts, withdrawalCall("run-1", matterA, 5_000, ""), agent.ErrMissingDep},
		{"nil matter contacts refused", sourced(), nil, withdrawalCall("run-1", matterA, 5_000, ""), agent.ErrMissingDep},
		{"free-text payee refused", sourced(), writeContacts, payeeCall("run-1", matterA, 5_000, "", "Firm operating account"), agent.ErrPayeeNotContact},
		{"cash payee refused", sourced(), writeContacts, payeeCall("run-1", matterA, 5_000, "", "cash"), agent.ErrPayeeNotContact},
		{"empty payee refused", sourced(), writeContacts, payeeCall("run-1", matterA, 5_000, "", ""), agent.ErrInvalidInput},
		{"another matter's contact refused", sourced(), writeContacts, payeeCall("run-1", matterA, 5_000, "", payeeW), agent.ErrPayeeNotContact},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools, wd := newWriteToolsWith(t, tc.pv, tc.contacts)
			res, err := tools.Call(context.Background(), tc.call)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				// No withdrawal row: the trust module was never asked.
				if wd.requests != 0 || res.GateID != "" {
					t.Fatalf("refused call made %d requests, gate %q", wd.requests, res.GateID)
				}
				return
			}
			if wd.requests != 1 || res.GateID == "" {
				t.Fatalf("requests = %d, gate %q; want one proposal", wd.requests, res.GateID)
			}
		})
	}
}

func feeCall(purpose, payee, billID string, cents int64) agent.Call {
	in := map[string]any{"matter_id": matterA, "amount_cents": cents, "purpose_category": purpose, "payee": payee}
	if billID != "" {
		in["bill_id"] = billID
	}
	b, _ := json.Marshal(in)
	return agent.Call{RunID: "run-1", ActingFor: preparer, Tool: agent.ToolPrepareTrustWithdrawal, Input: b}
}

// rule: BIL-L-037
// rule: BIL-L-035
// The firm's general account is a payee only for fees on a delivered bill of the same matter,
// up to its unpaid balance (By-Law 9 s.9(1)); every other payee is a matter contact. The
// tool fails closed without the firm account or bills port, and no refusal records anything.
func TestBIL_L_037_FirmPayeeOnlyForDeliveredBillFees(t *testing.T) {
	cases := []struct {
		name    string
		call    agent.Call
		edit    func(*agent.WriteDeps)
		wantErr error
	}{
		{"fees with a delivered bill allowed", feeCall("fees", firmPayee, billDelivered, 300_000), nil, nil},
		{"fees over the bill balance refused", feeCall("fees", firmPayee, billDelivered, 300_001), nil, withdraw.ErrExceedsBillBalance},
		{"fees with an undelivered bill refused", feeCall("fees", firmPayee, billDraft, 300_000), nil, withdraw.ErrBillNotDelivered},
		// rule: BIL-L-035
		// A fee withdrawal names its bill: refused at the tool, whoever the payee.
		{"fees with no bill refused", feeCall("fees", firmPayee, "", 300_000), nil, agent.ErrFeesNeedBill},
		{"fees to a contact with no bill refused", feeCall("fees", payeeA, "", 300_000), nil, agent.ErrFeesNeedBill},
		{"fees with another matter's bill refused", feeCall("fees", firmPayee, "00000000-0000-0000-0000-0000000000b9", 300_000), nil, errBillNotOnMatter},
		{"disbursement to the firm refused", feeCall("disbursement", firmPayee, billDelivered, 300_000), nil, agent.ErrFirmPayeeNeedsBill},
		{"refund to the firm refused", feeCall("refund", firmPayee, "", 300_000), nil, agent.ErrFirmPayeeNeedsBill},
		{"contact payee allowed", feeCall("client_payment", payeeA, "", 300_000), nil, nil},
		{"nil firm account refused", feeCall("client_payment", payeeA, "", 300_000), func(d *agent.WriteDeps) { d.FirmAccount = nil }, agent.ErrMissingDep},
		{"nil bills refused", feeCall("client_payment", payeeA, "", 300_000), func(d *agent.WriteDeps) { d.Bills = nil }, agent.ErrMissingDep},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pv := agenttools.NewProvenance()
			pv.Observe("run-1", json.RawMessage(`{"balance_due_cents":300000,"other_cents":300001}`))
			tools, wd := newWriteToolsDeps(t, func(d *agent.WriteDeps) {
				d.Provenance = pv
				if tc.edit != nil {
					tc.edit(d)
				}
			})
			res, err := tools.Call(context.Background(), tc.call)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if wd.requests != 0 || res.GateID != "" {
					t.Fatalf("refused call made %d requests, gate %q", wd.requests, res.GateID)
				}
				return
			}
			if wd.requests != 1 || res.GateID == "" || wd.authorizes != 0 {
				t.Fatalf("requests = %d, authorizes = %d, gate %q; want one proposal only", wd.requests, wd.authorizes, res.GateID)
			}
			// The bill goes on to the trust module, which re-checks it at authorization.
			var in struct {
				BillID string `json:"bill_id"`
			}
			if err := json.Unmarshal(tc.call.Input, &in); err != nil || wd.last.BillID != in.BillID {
				t.Fatalf("request bill = %q, want %q (%v)", wd.last.BillID, in.BillID, err)
			}
		})
	}
}

// rule: ARC-U-027
// Authorize and RecordExecution are unreachable from the agent package: WriteDeps holds only
// the request side of the trust module, and no non-test source in the package calls either.
func TestARC_U_027_AgentCannotAuthorizeOrExecute(t *testing.T) {
	forbidden := map[string]bool{"Authorize": true, "RecordExecution": true}
	field, ok := reflect.TypeOf(agent.WriteDeps{}).FieldByName("Withdrawals")
	if !ok {
		t.Fatal("WriteDeps has no Withdrawals")
	}
	for name := range forbidden {
		if _, ok := field.Type.MethodByName(name); ok {
			t.Fatalf("WriteDeps.Withdrawals exposes %s", name)
		}
	}
	fset := token.NewFileSet()
	var found []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && forbidden[sel.Sel.Name] {
				found = append(found, fset.Position(sel.Pos()).String()+" "+sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("agent package reaches the trust effect: %v", found)
	}
	tools, wd := newWriteTools(t)
	if _, err := tools.Call(context.Background(), withdrawalCall("run-1", matterA, 5_000, "")); err != nil {
		t.Fatal(err)
	}
	if wd.authorizes != 0 || wd.executions != 0 {
		t.Fatalf("agent path authorized %d / executed %d withdrawals", wd.authorizes, wd.executions)
	}
}
