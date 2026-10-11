package agentfake

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
)

func setup() *Fake {
	f := New(
		agent.ToolSpec{Name: "search_matters", Kind: agent.Read, Capability: "matters.view"},
		agent.ToolSpec{Name: "propose_withdrawal", Kind: agent.Write, Capability: "trust.request", GateKind: "trust.withdrawal"},
	)
	f.Grant("clerk", "matters.view")
	f.Grant("clerk", "trust.request")
	f.Hide("m-walled")
	f.SetResult("search_matters", agent.Result{
		Output:    json.RawMessage(`[{"id":"m1"}]`),
		Citations: []agent.Citation{{VersionID: "v1", Page: 1, FromLine: 1, ToLine: 2}},
	})
	return f
}

func TestCall(t *testing.T) {
	tests := []struct {
		name     string
		call     agent.Call
		wantErr  error
		wantGate bool
	}{
		{"read returns citations", agent.Call{RunID: "r1", ActingFor: "clerk", Tool: "search_matters", Input: json.RawMessage(`{}`)}, nil, false},
		{"walled subject", agent.Call{RunID: "r1", ActingFor: "clerk", Tool: "search_matters", Input: json.RawMessage(`{"id":"m-walled"}`)}, agent.ErrNotVisible, false},
		{"write only proposes", agent.Call{RunID: "r1", ActingFor: "clerk", Tool: "propose_withdrawal", Input: json.RawMessage(`{"id":"m1"}`)}, nil, true},
		{"unknown tool", agent.Call{ActingFor: "clerk", Tool: "pay_out", Input: json.RawMessage(`{}`)}, agent.ErrUnknownTool, false},
		{"no capability", agent.Call{ActingFor: "guest", Tool: "search_matters", Input: json.RawMessage(`{}`)}, agent.ErrForbidden, false},
		{"input not an object", agent.Call{ActingFor: "clerk", Tool: "search_matters", Input: json.RawMessage(`[1]`)}, agent.ErrInvalidInput, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := setup()
			r, err := f.Call(context.Background(), tt.call)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if (r.GateID != "") != tt.wantGate {
				t.Fatalf("GateID = %q, want gate %v", r.GateID, tt.wantGate)
			}
			if !tt.wantGate && len(r.Citations) == 0 {
				t.Fatal("read result without citation")
			}
			if tt.wantGate {
				p, ok := f.Proposal(r.GateID)
				if !ok || p.Decision != "" || p.Preparer != "clerk" || p.GateKind != "trust.withdrawal" {
					t.Fatalf("proposal = %+v, %v", p, ok)
				}
			}
		})
	}
}

func TestDecided(t *testing.T) {
	tests := []struct {
		name    string
		by      string
		gate    string
		wantRun string
		wantErr error
	}{
		{"other user approves, run resumes", "lawyer", "", "r1", nil},
		{"acting-for user cannot approve", "clerk", "", "", agent.ErrSelfApproval},
		{"unknown gate", "lawyer", "gate-x", "", agent.ErrUnknownGate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := setup()
			r, err := f.Call(ctx, agent.Call{RunID: "r1", ActingFor: "clerk", Tool: "propose_withdrawal", Input: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			gate := tt.gate
			if gate == "" {
				gate = r.GateID
			}
			run, err := f.Decided(ctx, agent.GateDecided{GateID: gate, Decision: agent.Approved, By: tt.by})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if run != tt.wantRun {
				t.Fatalf("run = %q, want %q", run, tt.wantRun)
			}
			if err == nil {
				if _, err := f.Decided(ctx, agent.GateDecided{GateID: gate, Decision: agent.Approved, By: tt.by}); !errors.Is(err, agent.ErrUnknownGate) {
					t.Fatalf("second decision err = %v", err)
				}
			}
		})
	}
}

func TestSpecsSorted(t *testing.T) {
	s := setup().Specs()
	if len(s) != 2 || s[0].Name != "propose_withdrawal" || s[1].Name != "search_matters" {
		t.Fatalf("Specs = %+v", s)
	}
}
