package agenttools_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestARC_I_038_RunScopesReturnedAndRecordDraft(t *testing.T) {
	returned := agenttools.Citation{VersionID: "v1", Page: 2, FromLine: 1, ToLine: 10}
	scope := agenttools.NewScope("run-1", nil)
	scope.AddCitations([]agenttools.Citation{returned})
	ctx := agenttools.WithScope(context.Background(), scope)
	tests := []struct {
		name  string
		ctx   context.Context
		runID string
		c     agenttools.Citation
		want  bool
		err   error
	}{
		{"same range", ctx, "run-1", returned, true, nil},
		{"inside the range", ctx, "run-1", agenttools.Citation{VersionID: "v1", Page: 2, FromLine: 3, ToLine: 4}, true, nil},
		{"past the range", ctx, "run-1", agenttools.Citation{VersionID: "v1", Page: 2, FromLine: 3, ToLine: 11}, false, nil},
		{"other page", ctx, "run-1", agenttools.Citation{VersionID: "v1", Page: 3, FromLine: 1, ToLine: 2}, false, nil},
		{"other version", ctx, "run-1", agenttools.Citation{VersionID: "v2", Page: 2, FromLine: 1, ToLine: 2}, false, nil},
		{"other run", ctx, "run-2", returned, false, agenttools.ErrNoRunScope},
		{"no scope", context.Background(), "run-1", returned, false, agenttools.ErrNoRunScope},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := agenttools.RunScopes{}.Returned(tt.ctx, tt.runID, tt.c)
			if !errors.Is(err, tt.err) || got != tt.want {
				t.Fatalf("Returned = %v, %v; want %v, %v", got, err, tt.want, tt.err)
			}
			rerr := agenttools.RunScopes{}.RecordDraft(tt.ctx, tt.runID, "d-"+tt.name)
			if !errors.Is(rerr, tt.err) {
				t.Fatalf("RecordDraft err = %v, want %v", rerr, tt.err)
			}
			if tt.err == nil && !scope.HasDraft("d-"+tt.name) {
				t.Fatalf("draft not recorded in the run scope")
			}
		})
	}
}

func TestARC_I_038_CallRecordsReturnedCitations(t *testing.T) {
	cited := agenttools.Citation{VersionID: "v1", Page: 1, FromLine: 2, ToLine: 5}
	reg := agenttools.NewRegistry(agenttoolsfake.New())
	if err := reg.Register(agenttools.Tool{
		Name: "read_doc", Kind: registry.ToolRead, Input: []byte(`{"type":"object"}`),
		Handle: func(context.Context, agenttools.Principal, json.RawMessage) (agenttools.ToolResult, error) {
			return agenttools.ToolResult{Output: json.RawMessage(`{}`), Citations: []agenttools.Citation{cited}}, nil
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	tests := []struct {
		name string
		p    agenttools.Principal
		want bool
	}{
		{"agent run records", agenttools.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "u1"}, true},
		{"user principal records nothing", agenttools.Principal{Kind: registry.PrincipalUser, UserID: "u1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := agenttools.NewScope("run-1", nil)
			ctx := agenttools.WithScope(context.Background(), scope)
			if _, err := reg.Call(ctx, tt.p, "read_doc", json.RawMessage(`{}`)); err != nil {
				t.Fatalf("Call: %v", err)
			}
			if got := scope.Cites(cited); got != tt.want {
				t.Fatalf("Cites = %v, want %v", got, tt.want)
			}
		})
	}
}
