package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/kernel/registry"
)

// errWrapped is what the test wrap returns, so a call shows which handler ran.
var errWrapped = errors.New("wrapped handler ran")

// TestHandlerSetFeatureWrap checks the per-(feature, tool) resolution: the wrapped feature
// runs its wrap, another feature and the manual path run the default, and an agent call with
// no feature fails closed.
//
// rule: ARC-U-027
func TestHandlerSetFeatureWrap(t *testing.T) {
	base, err := agent.NewHandlerSet(agent.Handler{Name: "draft_document", Build: func(agent.Deps) (registry.Tool, error) {
		return registry.Tool{Name: "draft_document", Kind: registry.ToolWrite, Handle: func(context.Context, registry.Principal, json.RawMessage) (registry.ToolResult, error) {
			return registry.ToolResult{Output: json.RawMessage(`"default"`)}, nil
		}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	wrap := func(_ agent.Deps, tl registry.Tool) (registry.Tool, error) {
		tl.Handle = func(context.Context, registry.Principal, json.RawMessage) (registry.ToolResult, error) {
			return registry.ToolResult{}, errWrapped
		}
		return tl, nil
	}
	set, err := base.WithFeatureWrap("F-TRU-06", "draft_document", wrap)
	if err != nil {
		t.Fatal(err)
	}
	if base.Wrapped("F-TRU-06", "draft_document") {
		t.Fatal("WithFeatureWrap changed the set it was called on")
	}
	if !set.Wrapped("F-TRU-06", "draft_document") || set.Wrapped("F-MAT-08", "draft_document") {
		t.Fatal("Wrapped does not report the one wrap")
	}
	h, _ := set.For("draft_document")
	tool, err := h.Build(agent.NewDeps())
	if err != nil {
		t.Fatal(err)
	}
	agentP := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "u-1"}
	tests := []struct {
		name    string
		feature string
		p       registry.Principal
		want    error
	}{
		{"wrapped feature runs its wrap", "F-TRU-06", agentP, errWrapped},
		{"other feature runs the default", "F-MAT-08", agentP, nil},
		{"agent call without a feature fails closed", "", agentP, agent.ErrNoFeature},
		{"manual path runs the default", "", registry.Principal{Kind: registry.PrincipalUser, UserID: "u-1"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.feature != "" {
				ctx = agent.WithFeature(ctx, tt.feature)
			}
			_, err := tool.Handle(ctx, tt.p, json.RawMessage(`{}`))
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("Handle = %v, want %v", err, tt.want)
			}
		})
	}
	for _, bad := range []struct {
		name, feature, tool string
		want                error
	}{
		{"duplicate wrap", "F-TRU-06", "draft_document", agent.ErrDuplicateWrap},
		{"tool not in the set", "F-TRU-06", "draft_email", agent.ErrMissingDep},
		{"no feature", "", "draft_document", agent.ErrMissingDep},
	} {
		if _, err := set.WithFeatureWrap(bad.feature, bad.tool, wrap); !errors.Is(err, bad.want) {
			t.Fatalf("%s: err = %v, want %v", bad.name, err, bad.want)
		}
	}
}
