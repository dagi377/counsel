package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// publishedPacks is a fixed published-pack list.
type publishedPacks []deadline.Pack

func (p publishedPacks) PublishedPacks(context.Context) ([]deadline.Pack, error) { return p, nil }

// computeWorld loads synthetic packs into a fake engine and publishes some of them. The
// "drift" pack is published at version 1 while the engine holds version 2. It returns a
// Registry with the real compute_deadline wrapped so its results record into pv, and the
// engine, to check nothing was saved.
func computeWorld(t *testing.T, actorCaps map[string]bool) (*agenttools.Registry, *agenttools.Provenance, *deadlinefake.Fake) {
	t.Helper()
	ctx := context.Background()
	engine := deadlinefake.New()
	civil := deadline.Pack{ID: "syn-civil", Version: "2026.1", Rules: []deadline.Rule{
		{ID: "defence", Mode: deadline.Calendar, Days: 20, Roll: deadline.RollForward},
		{ID: "shared", Mode: deadline.Clear, Days: 10},
	}}
	family := deadline.Pack{ID: "syn-family", Version: "2026.1", Rules: []deadline.Rule{{ID: "shared", Mode: deadline.Calendar, Days: 30}}}
	drift := deadline.Pack{ID: "syn-drift", Version: "1", Rules: []deadline.Rule{{ID: "notice", Mode: deadline.Calendar, Days: 5}}}
	unpublished := deadline.Pack{ID: "syn-draft", Version: "1", Rules: []deadline.Rule{{ID: "draft-rule", Mode: deadline.Calendar, Days: 1}}}
	newer := drift
	newer.Version = "2"
	for _, p := range []deadline.Pack{civil, family, newer, unpublished} {
		if err := engine.LoadPack(ctx, p); err != nil {
			t.Fatalf("load: %v", err)
		}
	}
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	tool, err := agent.ComputeDeadlineHandler().Build(agent.NewDeps(can, publishedPacks{civil, family, drift}, engine))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	pv := agenttools.NewProvenance()
	if tool, err = pv.ReadTool(tool); err != nil {
		t.Fatalf("provenance: %v", err)
	}
	reg := agenttools.NewRegistry(agenttoolsfake.New())
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return reg, pv, engine
}

// rule: ARC-I-036
// rule: ARC-I-038
func TestARC_I_036_ComputeDeadline(t *testing.T) {
	granted := []string{agent.CapDeadlinesCompute}
	view := map[string]bool{agent.CapDeadlinesCompute: true}
	cases := []struct {
		name      string
		grant     []string
		actorCaps map[string]bool
		in        string
		wantErr   error
		wantDue   []string
	}{
		// 2026-03-02 + 20 = 2026-03-22, a Sunday: rolled forward to Monday.
		{"bare rule", granted, view, `{"rule_id":"defence","trigger_date":"2026-03-02"}`, nil, []string{"2026-03-23"}},
		{"qualified rule", granted, view, `{"rule_id":"syn-family/shared","trigger_date":"2026-03-02"}`, nil, []string{"2026-04-01"}},
		{"published version named", granted, view, `{"rule_id":"defence","trigger_date":"2026-03-02","rule_pack_version":"2026.1"}`, nil, []string{"2026-03-23"}},
		{"other version named", granted, view, `{"rule_id":"defence","trigger_date":"2026-03-02","rule_pack_version":"2025.1"}`, deadline.ErrUnknownPack, nil},
		{"engine holds an unpublished version", granted, view, `{"rule_id":"notice","trigger_date":"2026-03-02"}`, deadline.ErrUnknownPack, nil},
		{"rule of an unpublished pack", granted, view, `{"rule_id":"draft-rule","trigger_date":"2026-03-02"}`, deadline.ErrUnknownRule, nil},
		{"unknown rule", granted, view, `{"rule_id":"nope","trigger_date":"2026-03-02"}`, deadline.ErrUnknownRule, nil},
		{"rule in two packs", granted, view, `{"rule_id":"shared","trigger_date":"2026-03-02"}`, agent.ErrInvalidInput, nil},
		{"run without the capability", nil, view, `{"rule_id":"defence","trigger_date":"2026-03-02"}`, agenttools.ErrMissingCapability, nil},
		{"acting user without the capability", granted, map[string]bool{}, `{"rule_id":"defence","trigger_date":"2026-03-02"}`, agent.ErrForbidden, nil},
		{"trigger missing", granted, view, `{"rule_id":"defence"}`, agenttools.ErrInvalidInput, nil},
		{"unknown field", granted, view, `{"rule_id":"defence","trigger_date":"2026-03-02","matter_id":"x"}`, agenttools.ErrInvalidInput, nil},
		{"bad date", granted, view, `{"rule_id":"defence","trigger_date":"02/03/2026"}`, agent.ErrInvalidInput, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg, pv, engine := computeWorld(t, tc.actorCaps)
			p := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-cd", ActingFor: clerk, Grant: tc.grant}
			res, err := reg.Call(context.Background(), p, agent.ToolComputeDeadline, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			// The tool never writes: no deadline is stored, whatever the outcome.
			if _, gerr := engine.Get(context.Background(), "dl-1"); !errors.Is(gerr, deadline.ErrNotFound) {
				t.Errorf("a deadline was saved: %v", gerr)
			}
			if err != nil {
				return
			}
			// rule: ARC-I-038
			if err := agenttools.CheckCitations(res); err != nil {
				t.Fatalf("citations: %v", err)
			}
			var out struct {
				Computed []struct {
					Due string `json:"due_date"`
				} `json:"computed"`
				Saved bool `json:"saved"`
			}
			if err := json.Unmarshal(res.Output, &out); err != nil {
				t.Fatalf("output: %v", err)
			}
			var due []string
			for _, c := range out.Computed {
				due = append(due, c.Due)
			}
			if !slices.Equal(due, tc.wantDue) || out.Saved {
				t.Errorf("due = %v saved %v, want %v unsaved", due, out.Saved, tc.wantDue)
			}
			// rule: ARC-U-027
			// Computed due dates are sourced for a later write in the same run.
			for _, d := range due {
				if err := pv.Check("run-cd", []string{"/due"}, json.RawMessage(`{"due":"`+d+`"}`)); err != nil {
					t.Errorf("due %s not in provenance: %v", d, err)
				}
			}
		})
	}
}
