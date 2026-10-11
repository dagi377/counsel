package trust_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// TestSourcedCents_ProductionWrap runs one draft through boot's production handler set
// (trubHandlers starts from boot.AgentHandlers). The wrap is per feature: a trust feature's
// draft with an invented cents figure is refused, while the same draft from a non-trust
// feature runs the default draft_document handler unchecked.
//
// rule: ARC-U-027
func TestSourcedCents_ProductionWrap(t *testing.T) {
	const invented = "Trust balance 480001 cents."
	nonTrust := trust.AnnualReportPrepDef()
	nonTrust.ID, nonTrust.Area, nonTrust.Fixture = "F-MAT-01", "matter", "testdata/ai/f-mat-01/non-trust-draft"
	tests := []struct {
		name    string
		trust   bool
		body    string
		refused bool
	}{
		{"trust feature, invented figure refused", true, invented, true},
		{"trust feature, sourced figure stored", true, trub06Body, false},
		{"non-trust feature, same invented figure stored", false, invented, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := trust.AnnualReportPrepDef()
			if !tt.trust {
				def = nonTrust
			}
			w := trubFeature(t, def, trub06Ledger)
			if len(w.fd.StandIns) != 0 {
				t.Fatalf("stand-ins %v: the production handlers must serve every tool", w.fd.StandIns)
			}
			calls := append(trubReads(w), trubDocumentWrites(t, w, "Trust worksheet", tt.body, trust.GateRegulatoryRelease)[:2]...)
			run := trubFire(t, w, calls, "fiscal year ended")
			if tt.refused {
				trubCheckRefused(t, w, run, agent.ToolDraftDocument, agenttools.ErrUnsourcedValue, 0)
				return
			}
			if len(run.Refused) != 0 || len(w.fd.Seed.Store.Drafts) != 1 {
				t.Fatalf("refused %v, drafts %d, tools %v, err %v; want none refused and one draft", run.Refused, len(w.fd.Seed.Store.Drafts), run.Tools, run.Err)
			}
		})
	}
}

// TestTRUB_SourcedCents checks the trust draft amount check on its own: every cents figure
// must be one the run read, and it fails closed.
//
// rule: ARC-U-027
func TestTRUB_SourcedCents(t *testing.T) {
	pv := agenttools.NewProvenance()
	pv.Observe("run-1", json.RawMessage(`{"available_cents": 480000, "overdrawn_cents": -4200, "balance_cents": 125000}`))
	tests := []struct {
		name  string
		pv    *agenttools.Provenance
		runID string
		text  string
		want  error
	}{
		{"no amount", pv, "run-1", "The ledger is in balance.", nil},
		{"read figures", pv, "run-1", "Balance 480000 cents; bill 125,000 cents.", nil},
		{"overdraft figure", pv, "run-1", "Overdrawn ledgers: -4200 cents.", nil},
		{"invented figure", pv, "run-1", "Balance 480001 cents.", agenttools.ErrUnsourcedValue},
		{"worked-out figure", pv, "run-1", "Shortfall 355000 cents.", agenttools.ErrUnsourcedValue},
		{"other run", pv, "run-2", "Balance 480000 cents.", agenttools.ErrUnsourcedValue},
		{"fraction of a cent", pv, "run-1", "Balance 480000.5 cents.", agenttools.ErrUnsourcedValue},
		{"dollar sign", pv, "run-1", "Balance $4,800.00.", agenttools.ErrUnsourcedValue},
		{"dollars word", pv, "run-1", "Balance 4800 dollars.", agenttools.ErrUnsourcedValue},
		{"no provenance", nil, "run-1", "The ledger is in balance.", agenttools.ErrUnsourcedValue},
		{"no run", pv, "", "The ledger is in balance.", agenttools.ErrUnsourcedValue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := trust.CheckSourcedCents(tt.pv, tt.runID, tt.text)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("CheckSourcedCents = %v, want %v", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "480001") {
				t.Fatalf("error names the figure: %v", err)
			}
		})
	}
}

// TestTRUB_SourcedDraft checks the draft wrapper: an agent's invented amount never reaches the
// handler, a user's manual draft is not checked, and only a write tool can be wrapped.
//
// rule: ARC-U-027
func TestTRUB_SourcedDraft(t *testing.T) {
	pv := agenttools.NewProvenance()
	pv.Observe("run-1", json.RawMessage(`{"available_cents": 480000}`))
	calls := 0
	write := registry.Tool{Name: "draft_document", Kind: registry.ToolWrite, Handle: func(context.Context, registry.Principal, json.RawMessage) (registry.ToolResult, error) {
		calls++
		return registry.ToolResult{Output: json.RawMessage(`{}`)}, nil
	}}
	wrapped, err := trust.SourcedCents(pv, write)
	if err != nil {
		t.Fatal(err)
	}
	agentP := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "u-1"}
	userP := registry.Principal{Kind: registry.PrincipalUser, UserID: "u-1"}
	tests := []struct {
		name      string
		p         registry.Principal
		in        string
		want      error
		wantCalls int
	}{
		{"agent sourced", agentP, `{"title": "Worksheet", "body_markdown": "Balance 480000 cents."}`, nil, 1},
		{"agent invented", agentP, `{"title": "Worksheet", "body_markdown": "Balance 999 cents."}`, agenttools.ErrUnsourcedValue, 0},
		{"agent invented in subject", agentP, `{"subject": "Top up 999 cents", "body_markdown": "Balance 480000 cents."}`, agenttools.ErrUnsourcedValue, 0},
		{"agent bad input", agentP, `[`, agenttools.ErrInvalidInput, 0},
		{"user manual draft", userP, `{"title": "Worksheet", "body_markdown": "Balance 999 cents."}`, nil, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls = 0
			_, err := wrapped.Handle(context.Background(), tt.p, json.RawMessage(tt.in))
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) || calls != tt.wantCalls {
				t.Fatalf("Handle = %v, %d calls; want %v, %d calls", err, calls, tt.want, tt.wantCalls)
			}
		})
	}
	read := registry.Tool{Name: "get_bill", Kind: registry.ToolRead, Handle: write.Handle}
	if _, err := trust.SourcedCents(pv, read); !errors.Is(err, agenttools.ErrBadTool) {
		t.Fatalf("wrap read tool = %v, want ErrBadTool", err)
	}
	if _, err := trust.SourcedCents(nil, write); !errors.Is(err, agenttools.ErrUnsourcedValue) {
		t.Fatalf("wrap without provenance = %v, want ErrUnsourcedValue", err)
	}
}
