package agent_test

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
)

const (
	preparer = "user-clerk"
	lawyer   = "user-lawyer"
	matterA  = "00000000-0000-0000-0000-00000000000a"
	matterW  = "00000000-0000-0000-0000-00000000000b"
	account  = "acct-1"
)

// countingWithdrawals records every call so tests can see that the agent path only requests.
type countingWithdrawals struct {
	*withdrawfake.Fake
	requests, authorizes, executions int
}

func (c *countingWithdrawals) Request(ctx context.Context, r withdraw.Request) (withdraw.Withdrawal, error) {
	c.requests++
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

func newWriteTools(t *testing.T) (*agent.WriteTools, *countingWithdrawals) {
	t.Helper()
	wf := withdrawfake.New(24 * time.Hour)
	wf.SetBalance(account, matterA, 500_000, 0)
	wd := &countingWithdrawals{Fake: wf}
	rf := rolesfake.New()
	rf.Wall(preparer, matterW)
	tools, err := agent.NewWriteTools(agent.WriteDeps{
		Can:          func(_ context.Context, user, capability string) (bool, error) { return user == preparer, nil },
		Walls:        rf,
		TrustAccount: func(context.Context, string) (string, error) { return account, nil },
		Withdrawals:  wd,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tools, wd
}

func withdrawalCall(run, matterID string, cents int64, method string) agent.Call {
	in := map[string]any{"matter_id": matterID, "amount_cents": cents, "purpose_category": "fees", "payee": "Firm operating account"}
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
