package agent_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestHandlersIsExplicitList(t *testing.T) {
	want := []string{agent.ToolDraftDocument, agent.ToolDraftEmail, agent.ToolDraftNote, agent.ToolListDocuments,
		agent.ToolListWorkItems, agent.ToolReportPlan, agent.ToolReportStep, agent.ToolSearchContacts, agent.ToolSearchDocuments,
		agent.ToolListDeadlines, agent.ToolComputeDeadline, agent.ToolListUnbilled, agent.ToolGetBill, agent.ToolGetTrustBalance,
		agent.ToolProposeTimeEntries, agent.ToolProposeDeadline, agent.ToolCreateTask, agent.ToolProposeConflictParties,
		agent.ToolSubmitForApproval, agent.ToolDraftBill, agent.ToolProposePaymentAllocation, agent.ToolSearchMatters,
		agent.ToolGetMatter, agent.ToolReadDocument, agent.ToolPrepareTrustWithdrawal}
	slices.Sort(want)
	if got := agent.HandlerNames(); !slices.Equal(got, want) {
		t.Fatalf("HandlerNames() = %v, want %v", got, want)
	}
	for _, n := range want {
		if _, ok := agent.Handlers().For(n); !ok {
			t.Errorf("Handlers().For(%q) missing", n)
		}
	}
}

func TestNewHandlerSet(t *testing.T) {
	build := func(agent.Deps) (registry.Tool, error) { return registry.Tool{}, nil }
	tests := []struct {
		name    string
		hs      []agent.Handler
		wantErr error
		want    []string
	}{
		{name: "empty", want: []string{}},
		{name: "two", hs: []agent.Handler{{Name: "b", Build: build}, {Name: "a", Build: build}}, want: []string{"a", "b"}},
		{name: "duplicate", hs: []agent.Handler{{Name: "a", Build: build}, {Name: "a", Build: build}}, wantErr: agent.ErrDuplicateHandler},
		{name: "no name", hs: []agent.Handler{{Build: build}}, wantErr: agent.ErrMissingDep},
		{name: "nil build", hs: []agent.Handler{{Name: "a"}}, wantErr: agent.ErrMissingDep},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := agent.NewHandlerSet(tt.hs...)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got := s.Names(); !slices.Equal(got, tt.want) {
				t.Fatalf("Names() = %v, want %v", got, tt.want)
			}
		})
	}
}
