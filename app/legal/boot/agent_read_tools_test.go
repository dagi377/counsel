package boot

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	rtRun    = "run-rt"
	rtClerk  = "user-clerk"
	rtMatter = "33333333-3333-4333-8333-333333333333"
)

// rtMatterTime is one approved, billable 90-minute entry at 35000 cents an hour.
type rtMatterTime struct{}

func (rtMatterTime) MatterTime(_ context.Context, matterID string) ([]agent.PricedEntry, error) {
	if matterID != rtMatter {
		return nil, nil
	}
	e := tk.TimeEntry{ID: "te-1", MatterID: rtMatter, UserID: "user-lawyer", Minutes: 90, BilledMinutes: 90, Billable: true, State: tk.StateApproved, Narrative: "Drafted claim", ActivityCode: "A101"}
	return []agent.PricedEntry{{Entry: e, RateCents: 35000, AmountCents: 52500}}, nil
}

// rule: ARC-U-027
// list_unbilled does not record its own results: boot's AgentTools wraps every read tool, so
// the cents it returns in a run are sourced for that run only, and an invented figure is not.
func TestAgentToolsRecordListUnbilled(t *testing.T) {
	p := withGate(fullPorts())
	p.Can = func(_ context.Context, user, capability string) (bool, error) {
		return user == rtClerk && capability == agent.CapBillingView, nil
	}
	p.Walls = rolesfake.New()
	p.MatterTime = rtMatterTime{}
	tools, err := AgentTools(p)
	if err != nil {
		t.Fatalf("AgentTools: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(context.Background(), rtRun, []string{"draft the bill"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	reg := agenttools.NewRegistry(plans)
	for _, tl := range tools {
		if tl.Name == agent.ToolListUnbilled {
			if err := reg.Register(tl); err != nil {
				t.Fatalf("register: %v", err)
			}
		}
	}
	principal := registry.Principal{Kind: registry.PrincipalAgent, RunID: rtRun, ActingFor: rtClerk, Grant: []string{agent.CapBillingView}}
	if _, err := reg.Call(context.Background(), principal, agent.ToolListUnbilled, json.RawMessage(`{"matter_id":"`+rtMatter+`"}`)); err != nil {
		t.Fatalf("list_unbilled: %v", err)
	}
	tests := []struct {
		name    string
		run     string
		cents   int64
		wantErr error
	}{
		{"amount read in the run", rtRun, 52500, nil},
		{"invented amount", rtRun, 12345, agenttools.ErrUnsourcedValue},
		{"amount read in another run", "run-other", 52500, agenttools.ErrUnsourcedValue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in, _ := json.Marshal(map[string]int64{"amount": tc.cents})
			if err := p.Provenance.Check(tc.run, []string{"/amount"}, in); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
