package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/schemas"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
)

// stubSet is a handler set with a stub handler for each name; reached records calls.
func stubSet(t *testing.T, names []string, buildErr error, reached map[string]int) agent.HandlerSet {
	t.Helper()
	hs := make([]agent.Handler, 0, len(names))
	for _, n := range names {
		hs = append(hs, agent.Handler{Name: n, Build: func(agent.Deps) (registry.Tool, error) {
			if buildErr != nil {
				return registry.Tool{}, buildErr
			}
			return registry.Tool{Name: n, Handle: func(context.Context, registry.Principal, json.RawMessage) (registry.ToolResult, error) {
				reached[n]++
				return registry.ToolResult{Output: json.RawMessage(`{"outcome":"proposal","ref":"p-1"}`)}, nil
			}}, nil
		}})
	}
	s, err := agent.NewHandlerSet(hs...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func a2Names(t *testing.T) []string {
	t.Helper()
	defs, err := schemas.Defs()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	slices.Sort(names)
	return names
}

// rule: ARC-I-035
func TestHandlersCoverEveryA2Tool(t *testing.T) {
	want := a2Names(t)
	if len(want) != schemas.Count {
		t.Fatalf("a2 tools = %d, want %d", len(want), schemas.Count)
	}
	if got := agent.HandlerNames(); !slices.Equal(got, want) {
		t.Fatalf("HandlerNames() = %v, want every a2 tool %v", got, want)
	}
	for _, h := range []agent.Handler{agent.SearchMattersHandler(), agent.GetMatterHandler(), agent.ReadDocumentHandler(), agent.PrepareTrustWithdrawalHandler()} {
		if h.Build == nil || !slices.Contains(want, h.Name) {
			t.Errorf("handler %q: not an a2 tool or no Build", h.Name)
		}
	}
}

// rule: ARC-I-035
func TestRegisterHandlers(t *testing.T) {
	all := a2Names(t)
	errBoom := errors.New("boom")
	tests := []struct {
		name     string
		set      func(t *testing.T) agent.HandlerSet
		deps     agent.Deps
		wantErr  error
		wantRegs int
	}{
		{name: "every tool", set: func(t *testing.T) agent.HandlerSet { return stubSet(t, all, nil, map[string]int{}) }, wantRegs: schemas.Count},
		{name: "one missing", set: func(t *testing.T) agent.HandlerSet {
			return stubSet(t, slices.DeleteFunc(slices.Clone(all), func(n string) bool { return n == agent.ToolSearchMatters }), nil, map[string]int{})
		}, wantErr: schemas.ErrMissingHandler},
		{name: "build fails", set: func(t *testing.T) agent.HandlerSet { return stubSet(t, all, errBoom, map[string]int{}) }, wantErr: errBoom},
		{name: "app set without ports", set: func(*testing.T) agent.HandlerSet { return agent.Handlers() }, deps: agent.NewDeps(), wantErr: agent.ErrMissingDep},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := agenttools.NewRegistry(agenttoolsfake.New())
			err := agent.RegisterHandlers(reg, tt.set(t), tt.deps)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("RegisterHandlers: %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			user := registry.Principal{Kind: registry.PrincipalUser, UserID: "u1"}
			for _, n := range all {
				// An unknown tool fails ErrUnknownTool; a registered one fails on input or runs.
				if _, err := reg.Call(context.Background(), user, n, json.RawMessage(`{"bad":1}`)); errors.Is(err, agenttools.ErrUnknownTool) {
					t.Errorf("%s not registered", n)
				}
			}
		})
	}
}

// rule: ARC-I-036
func TestAgentGrantIntersectsActingUser(t *testing.T) {
	caps := map[string][]string{
		"clerk":  {"matters.view", "documents.view"},
		"lawyer": {"matters.view", "documents.view", "billing.draft"},
	}
	can := agent.CapabilityCheck(func(_ context.Context, user, c string) (bool, error) {
		return slices.Contains(caps[user], c), nil
	})
	role := []string{"matters.view", "billing.draft", "trust.request"}
	in := json.RawMessage(`{"matter_id":"00000000-0000-4000-8000-000000000001","period_to":"2026-09-30"}`)
	tests := []struct {
		user      string
		wantGrant []string
		wantErr   error
	}{
		{user: "clerk", wantGrant: []string{"matters.view"}, wantErr: agenttools.ErrMissingCapability},
		{user: "lawyer", wantGrant: []string{"matters.view", "billing.draft"}},
	}
	for _, tt := range tests {
		t.Run(tt.user, func(t *testing.T) {
			ctx := context.Background()
			grant, err := agent.AgentGrant(ctx, can, tt.user, role)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(grant, tt.wantGrant) {
				t.Fatalf("grant = %v, want %v", grant, tt.wantGrant)
			}
			plans := agenttoolsfake.New()
			reg := agenttools.NewRegistry(plans)
			reached := map[string]int{}
			if err := agent.RegisterHandlers(reg, stubSet(t, a2Names(t), nil, reached), agent.NewDeps()); err != nil {
				t.Fatal(err)
			}
			if _, err := plans.ReportPlan(ctx, "run-1", []string{"draft the bill"}); err != nil {
				t.Fatal(err)
			}
			p := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: tt.user, Grant: grant}
			_, err = reg.Call(ctx, p, agent.ToolDraftBill, in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("draft_bill: %v, want %v", err, tt.wantErr)
			}
			if wantReached := tt.wantErr == nil; (reached[agent.ToolDraftBill] == 1) != wantReached {
				t.Fatalf("handler reached %d times, want reached=%v", reached[agent.ToolDraftBill], wantReached)
			}
		})
	}
}
