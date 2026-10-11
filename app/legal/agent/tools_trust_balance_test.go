package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/app/legal/trust/withdraw/withdrawfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	trustMatter  = "c1111111-1111-4111-8111-111111111111"
	trustOther   = "c2222222-2222-4222-8222-222222222222"
	trustWalled  = "c3333333-3333-4333-8333-333333333333"
	trustGeneral = "acct-general"
	trustSep     = "acct-separate"
	trustRun     = "run-trust"
)

type matterAccounts map[string][]string

func (m matterAccounts) TrustAccountsOf(_ context.Context, matterID string) ([]string, error) {
	return m[matterID], nil
}

type trustWorld struct {
	reg *agenttools.Registry
	pv  *agenttools.Provenance
}

// newTrustWorld registers the real get_trust_balance over the withdraw fake. trustMatter
// holds 120000 + 5000 cents on two accounts; trustOther (visible) and trustWalled (walled from
// clerk) hold money on the general account too.
func newTrustWorld(t *testing.T, actorCaps map[string]bool) trustWorld {
	t.Helper()
	w := withdrawfake.New(24 * time.Hour)
	w.SetBalance(trustGeneral, trustMatter, 120000, 0)
	w.SetBalance(trustSep, trustMatter, 5000, 700)
	w.SetBalance(trustGeneral, trustOther, 999900, 0)
	w.SetBalance(trustGeneral, trustWalled, 777700, 0)
	accounts := matterAccounts{
		trustMatter: {trustGeneral, trustSep}, trustOther: {trustGeneral}, trustWalled: {trustGeneral},
	}
	walls := rolesfake.New()
	walls.Wall(clerk, trustWalled)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	pv := agenttools.NewProvenance()
	tool, err := agent.GetTrustBalanceHandler().Build(agent.NewDeps(can, walls, w, accounts, pv))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	reg := agenttools.NewRegistry(agenttoolsfake.New())
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return trustWorld{reg: reg, pv: pv}
}

func torontoToday(t *testing.T) string {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatalf("zone: %v", err)
	}
	return time.Now().In(loc).Format(time.DateOnly)
}

// rule: ARC-I-036
// rule: ARC-I-038
func TestARC_I_036_GetTrustBalance(t *testing.T) {
	view := map[string]bool{"trust.view": true}
	grant := []string{"trust.view"}
	agentP := func(g []string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: trustRun, ActingFor: clerk, Grant: g}
	}
	in := func(id string) string { return `{"matter_id":"` + id + `"}` }
	today := torontoToday(t)
	cases := []struct {
		name      string
		p         registry.Principal
		actorCaps map[string]bool
		in        string
		wantErr   error
		wantCents int64
	}{
		{"agent reads one matter", agentP(grant), view, in(trustMatter), nil, 125000},
		{"as of today", agentP(grant), view, `{"matter_id":"` + trustMatter + `","as_of":"` + today + `"}`, nil, 125000},
		{"manual path", registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}, view, in(trustMatter), nil, 125000},
		{"walled matter", agentP(grant), view, in(trustWalled), agent.ErrNotVisible, 0},
		{"walled matter on the manual path", registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}, view, in(trustWalled), agent.ErrNotVisible, 0},
		{"past as_of refused", agentP(grant), view, `{"matter_id":"` + trustMatter + `","as_of":"2020-01-31"}`, agent.ErrInvalidInput, 0},
		{"run without the capability", agentP(nil), view, in(trustMatter), agenttools.ErrMissingCapability, 0},
		{"acting user without the capability", agentP(grant), map[string]bool{}, in(trustMatter), agent.ErrForbidden, 0},
		{"matter id missing", agentP(grant), view, `{}`, agenttools.ErrInvalidInput, 0},
		{"second matter id", agentP(grant), view, `{"matter_id":"` + trustMatter + `","other_matter_id":"` + trustOther + `"}`, agenttools.ErrInvalidInput, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newTrustWorld(t, tc.actorCaps)
			res, err := w.reg.Call(context.Background(), tc.p, agent.ToolGetTrustBalance, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			// rule: ARC-I-038
			if err := agenttools.CheckCitations(res); err != nil {
				t.Fatalf("citations: %v", err)
			}
			var out struct {
				MatterID       string `json:"matter_id"`
				AsOf           string `json:"as_of"`
				AvailableCents int64  `json:"available_cents"`
				Accounts       []struct {
					TrustAccountID string `json:"trust_account_id"`
					AvailableCents int64  `json:"available_cents"`
				} `json:"accounts"`
			}
			if err := json.Unmarshal(res.Output, &out); err != nil {
				t.Fatalf("output: %v", err)
			}
			if out.MatterID != trustMatter || out.AsOf != today || out.AvailableCents != tc.wantCents || len(out.Accounts) != 2 {
				t.Errorf("output = %s, want %d cents on two accounts", res.Output, tc.wantCents)
			}
		})
	}
}

// The balance enters the run's provenance; another matter's balance, never read, does not.
func TestGetTrustBalanceFeedsProvenance(t *testing.T) {
	w := newTrustWorld(t, map[string]bool{"trust.view": true})
	p := registry.Principal{Kind: registry.PrincipalAgent, RunID: trustRun, ActingFor: clerk, Grant: []string{"trust.view"}}
	if _, err := w.reg.Call(context.Background(), p, agent.ToolGetTrustBalance, json.RawMessage(`{"matter_id":"`+trustMatter+`"}`)); err != nil {
		t.Fatalf("call: %v", err)
	}
	cases := []struct {
		name    string
		cents   int64
		wantErr error
	}{
		{"matter total", 125000, nil},
		{"one account", 5000, nil},
		{"other matter balance", 999900, agenttools.ErrUnsourcedValue},
		{"walled matter balance", 777700, agenttools.ErrUnsourcedValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := json.RawMessage(fmt.Sprintf(`{"amount_cents":%d}`, tc.cents))
			if err := w.pv.Check(trustRun, []string{"/amount_cents"}, in); !errors.Is(err, tc.wantErr) {
				t.Fatalf("check err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
