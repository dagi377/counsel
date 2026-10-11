package trust_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// truReconDraft is the id the SeedStore gives the run's one draft.
const truReconDraft = "draft-document-1"

// truReconBody is a package that lists every figure as a read returned it: the overdrawn
// ledger first, with its sign.
const truReconBody = "Overdrawn ledgers: matter 2026-0102 at -5000 cents.\n" +
	"Matter 2026-0101: 250000 cents.\n" +
	"Bill paid from trust: total 113000 cents, unpaid 0 cents."

// truReconCalls is the F-TRU-04 script over w: plan, reads, the package draft with body and
// its submission for kind.
func truReconCalls(w truWorld, body, kind string) []truCall {
	calls := []truCall{truPlan("read", "draft", "submit")}
	calls = append(calls, w.reads()...)
	return append(calls,
		truCall{agent.ToolDraftDocument, map[string]any{"matter_id": w.matter, "title": "Trust reconciliation package", "body_markdown": body}},
		truSubmit(truReconDraft, kind),
	)
}

// rule: AI-R-003
// rule: ARC-I-035
func TestFTRU04_DefMatchesSpec(t *testing.T) {
	d := trust.MonthEndReconciliationDef()
	truSpecSync(t, d, false)
	// The restated kind is G12, the reconciliation module's sign-off gate.
	if trust.GateSignOff != recon.GateSignOff {
		t.Fatalf("gate kind = %s, want %s", trust.GateSignOff, recon.GateSignOff)
	}
	if sf := truSpec(t, d.ID); !slices.Equal(sf.ToolGap, d.GateKinds) {
		t.Fatalf("tool-gap kinds = %v, want %v opened by draft_document then submit_for_approval", sf.ToolGap, d.GateKinds)
	}
}

// rule: AI-R-005
func TestFTRU04_Prompt(t *testing.T) {
	truPrompt(t, trust.MonthEndReconciliationDef(), "draft_document", "submit_for_approval", "integer cents", "Never hide an overdraft")
}

// F-TRU-04 starts when a statement is received and on a daily check that acts for the firm's
// designated trust lawyer; monthly-reconciliation runs it on request.
func TestFTRU04_TriggerBinding(t *testing.T) {
	b, err := trust.MonthEndReconciliationBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"feature", b.FeatureID, trust.FeatureMonthEndReconciliation},
		{"events", strings.Join(b.Events, ","), trust.EventStatementReceived},
		{"schedule", b.Cadence, trigger.CadenceDaily},
		{"acts for the designated lawyer", b.Designated, true},
		{"on request", strings.Join(trust.MonthEndReconciliationDef().Actions, ","), "monthly-reconciliation > Prepare package"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFTRU04_FromFeature(t *testing.T) {
	tests := []struct {
		name      string
		narrative string
	}{
		{"ok", "Prepared the mediation brief."},
		{"injection", truInjected},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := truFeature(t, trust.MonthEndReconciliationDef(), tc.narrative)
			// rule: ARC-I-035
			if len(w.fd.StandIns) != 0 {
				t.Fatalf("stand-ins = %v, want none", w.fd.StandIns)
			}
			calls := truReconCalls(w, truReconBody, trust.GateSignOff)
			run := w.fd.Fire(ctx, truScript(t, trust.FeatureMonthEndReconciliation, calls, "Package drafted for sign-off."), "run-f-tru-04-"+tc.name, "statement received for 2026-09")
			if run.Err != nil || len(run.Refused) != 0 || len(run.StandIns) != 0 {
				t.Fatalf("run err = %v, refused = %v, stand-ins = %v", run.Err, run.Refused, run.StandIns)
			}
			// rule: ARC-I-038
			// The run makes exactly the scripted calls: injected text adds none.
			if want := truToolNames(calls); !slices.Equal(run.Tools, want) {
				t.Fatalf("tools = %v, want %v", run.Tools, want)
			}
			// rule: ARC-I-039
			if run.PlanAt != 0 || run.FirstWrite != slices.Index(run.Tools, agent.ToolDraftDocument) {
				t.Fatalf("plan at %d, first write %d: report_plan must precede draft_document", run.PlanAt, run.FirstWrite)
			}
			if d := w.fd.Seed.Store.Drafts; len(d) != 1 || d[0].ID != truReconDraft || d[0].PreparedBy != agenttest.FeatureActingFor {
				t.Fatalf("drafts = %+v, want the package prepared by the acting user", d)
			}
			if len(run.GateIDs) != 1 {
				t.Fatalf("gates = %v, want one %s", run.GateIDs, trust.GateSignOff)
			}
			truCheckGate(t, w.fd, run.GateIDs[0], trust.GateSignOff)
			if tc.name != "injection" {
				return
			}
			// rule: ARC-I-038
			if err := agenttest.QuotedOnly(run.Run, truInjected); err != nil {
				t.Fatal(err)
			}
			if err := truAbsent(ctx, w.fd, run, truInjected); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// rule: ARC-U-027
// rule: BIL-L-036
// rule: ARC-I-035
func TestFTRU04_FromFeatureRefusesWrites(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		kind    string
		refused string
		wantErr error
		drafts  int
	}{
		{"gate kind outside the Def", truReconBody, "trust.withdrawal", agent.ToolSubmitForApproval, agenttools.ErrGateKindNotAllowed, 1},
		{"model-invented amount", truReconBody + "\nClient ledgers total 245000 cents.", trust.GateSignOff, agent.ToolDraftDocument, agenttools.ErrUnsourcedValue, 0},
		// rule: BIL-L-036
		// An overdraft written without its sign hides it: the unsigned figure was never read.
		{"overdraft without its sign", strings.Replace(truReconBody, "-5000 cents", "5000 cents", 1), trust.GateSignOff, agent.ToolDraftDocument, agenttools.ErrUnsourcedValue, 0},
		{"amount in dollars", strings.Replace(truReconBody, "250000 cents", "$2,500.00", 1), trust.GateSignOff, agent.ToolDraftDocument, agenttools.ErrUnsourcedValue, 0},
		{"date no read returned", truReconBody + "\nStatement closing 2026-09-30.", trust.GateSignOff, agent.ToolDraftDocument, agenttools.ErrUnsourcedValue, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := truFeature(t, trust.MonthEndReconciliationDef(), "Prepared the mediation brief.")
			calls := truReconCalls(w, tt.body, tt.kind)
			run := w.fd.Fire(context.Background(), truScript(t, trust.FeatureMonthEndReconciliation, calls, "Done."), "run-f-tru-04-refused", "statement received for 2026-09")
			if r, ok := truRefused(run, tt.refused); !ok || !errors.Is(r.Err, tt.wantErr) {
				t.Fatalf("refused = %+v, want %s refused with %v", run.Refused, tt.refused, tt.wantErr)
			}
			// No proposal row: no gate, and no draft when the draft itself was refused.
			if len(run.GateIDs) != 0 || len(w.fd.Seed.Store.Drafts) != tt.drafts {
				t.Fatalf("gates = %v, drafts = %d; want none and %d", run.GateIDs, len(w.fd.Seed.Store.Drafts), tt.drafts)
			}
		})
	}
}

// rule: AI-R-003
func TestFTRU04_PrepareOnly(t *testing.T) {
	truPrepareOnly(t, "ai_f_tru_04.go")
}

// rule: ARC-U-027
// rule: AI-R-003
func TestSourcedCents(t *testing.T) {
	pv := agenttools.NewProvenance()
	pv.Observe("run-1", []byte(`{"available_cents": -5000, "as_of": "2026-10-05", "total_cents": 113000}`))
	var ran int
	write := registry.Tool{Name: "draft_document", Kind: registry.ToolWrite, Handle: func(context.Context, registry.Principal, json.RawMessage) (registry.ToolResult, error) {
		ran++
		return registry.ToolResult{}, nil
	}}
	tool, err := trust.SourcedCents(pv, write)
	if err != nil {
		t.Fatal(err)
	}
	agentP := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "u"}
	tests := []struct {
		name    string
		p       registry.Principal
		in      string
		wantErr error
	}{
		{"read figures", agentP, `{"body_markdown": "Overdrawn at -5000 cents on 2026-10-05; bill 113000 cents."}`, nil},
		{"cents field", agentP, `{"amount_cents": 113000}`, nil},
		{"no amounts", agentP, `{"body_markdown": "Nothing to report."}`, nil},
		{"invented amount", agentP, `{"body_markdown": "Top up 7000 cents."}`, agenttools.ErrUnsourcedValue},
		{"invented cents field", agentP, `{"amount_cents": 7000}`, agenttools.ErrUnsourcedValue},
		{"overdraft unsigned", agentP, `{"body_markdown": "Overdrawn by 5000 cents."}`, agenttools.ErrUnsourcedValue},
		{"dollars", agentP, `{"body_markdown": "Bill $1,130.00."}`, agenttools.ErrUnsourcedValue},
		{"invented date", agentP, `{"body_markdown": "As of 2026-10-31."}`, agenttools.ErrUnsourcedValue},
		{"another run's figures", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-2"}, `{"amount_cents": 113000}`, agenttools.ErrUnsourcedValue},
		{"user on the manual path", registry.Principal{Kind: registry.PrincipalUser, UserID: "u"}, `{"amount_cents": 7000}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := ran
			_, err := tool.Handle(context.Background(), tt.p, json.RawMessage(tt.in))
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if wantRan := tt.wantErr == nil; (ran > before) != wantRan {
				t.Fatalf("handler ran = %v, want %v", ran > before, wantRan)
			}
		})
	}
	// rule: AI-R-003
	// Fail closed: no provenance, or a tool that is not a write, builds nothing.
	for _, bad := range []struct {
		name string
		pv   *agenttools.Provenance
		tool registry.Tool
	}{
		{"no provenance", nil, write},
		{"read tool", pv, registry.Tool{Name: "get_bill", Kind: registry.ToolRead, Handle: write.Handle}},
	} {
		if _, err := trust.SourcedCents(bad.pv, bad.tool); !errors.Is(err, trust.ErrPortMissing) {
			t.Fatalf("%s: err = %v, want ErrPortMissing", bad.name, err)
		}
	}
}
