package billing

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// bil08Draft is the draft_document call of an F-BIL-08 script whose credit note states body.
func bil08Draft(body string) bilcCall {
	return bilcCall{name: agent.ToolDraftDocument, input: `{"matter_id":"` + agenttest.TrustMatter +
		`","title":"Credit note for bill SYN-2026-0003","body_markdown":"` + body + `"}`}
}

// bil08Submit is the submit_for_approval call on the run's first draft for gate kind.
func bil08Submit(kind string) bilcCall {
	return bilcCall{name: agent.ToolSubmitForApproval, input: `{"subject_type":"draft","subject_id":"draft-document-1","gate_kind":"` + kind + `"}`}
}

// bil08Body is a credit note stating only figures get_bill returns: the bill's total, fees
// and tax.
const bil08Body = "Credit note for bill SYN-2026-0003. Reason: write-off agreed with the client. " +
	"Credit $1,130.00: fees $1,000.00 and tax $130.00 as billed."

// rule: ARC-I-035
// rule: ARC-I-039
// rule: ARC-U-027
// rule: AI-R-003
func TestFBIL08_FromFeature(t *testing.T) {
	head := []bilcCall{bilcPlan("bill", "draft", "submit"), bilcReads[0]}
	cases := []struct {
		name       string
		narrative  string
		calls      []bilcCall
		wantRefuse map[string]error // tool -> refusal
		wantGates  int
	}{
		{name: "ok", narrative: "Settlement review", calls: []bilcCall{bil08Draft(bil08Body), bil08Submit(GateCreditNote)}, wantGates: 1},
		// rule: ARC-U-027
		// A model-invented amount stores no draft and opens no gate.
		{name: "model-invented amount", narrative: "Settlement review",
			calls:      []bilcCall{bil08Draft("Credit note for bill SYN-2026-0003. Credit $1,129.00 as agreed.")},
			wantRefuse: map[string]error{agent.ToolDraftDocument: agenttools.ErrUnsourcedValue}},
		// A tax figure the agent worked out is a made-up amount too: tax is the engine's.
		{name: "computed tax", narrative: "Settlement review",
			calls:      []bilcCall{bil08Draft("Credit note for bill SYN-2026-0003. Credit $500.00 plus tax $65.00.")},
			wantRefuse: map[string]error{agent.ToolDraftDocument: agenttools.ErrUnsourcedValue}},
		// rule: AI-R-003
		// A gate kind outside the Def is refused before anything opens.
		{name: "kind outside the Def", narrative: "Settlement review",
			calls:      []bilcCall{bil08Draft(bil08Body), bil08Submit(GateCreditNote), bil08Submit(GateTrustFeeWithdrawal)},
			wantRefuse: map[string]error{agent.ToolSubmitForApproval: agenttools.ErrGateKindNotAllowed}, wantGates: 1},
		// rule: ARC-I-038
		{name: "injection", narrative: "Settlement review. " + bilcInjected,
			calls: []bilcCall{bil08Draft(bil08Body), bil08Submit(GateCreditNote)}, wantGates: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			def := DraftCreditNoteDef()
			fd := bilcFeature(t, def, c.narrative, CreditNoteDraftHandler())
			script := append(slices.Clone(head), c.calls...)
			run := fd.Fire(ctx, bilcScript(t, def.ID, script, "Drafted the credit note for approval."),
				"run-fbil08-"+strings.ReplaceAll(c.name, " ", "-"), ActionDraftCreditNote+": bill "+bilcBill)
			if run.Err != nil {
				t.Fatalf("run: %v", run.Err)
			}
			// rule: ARC-I-038
			if !slices.Equal(run.Tools, bilcNames(script)) {
				t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, bilcNames(script))
			}
			if len(run.Refused) != len(c.wantRefuse) {
				t.Fatalf("refused = %v, want %v", run.Refused, c.wantRefuse)
			}
			for _, r := range run.Refused {
				if want := c.wantRefuse[r.Tool]; want == nil || !errors.Is(r.Err, want) {
					t.Fatalf("refused %s: %v, want %v", r.Tool, r.Err, want)
				}
			}
			if len(run.GateIDs) != c.wantGates {
				t.Fatalf("gates = %v, want %d", run.GateIDs, c.wantGates)
			}
			if c.wantGates == 0 {
				if len(fd.Seed.Store.Drafts) != 0 {
					t.Fatalf("refused amount stored drafts %+v", fd.Seed.Store.Drafts)
				}
				return
			}
			// rule: ARC-I-039
			if run.PlanAt != 0 || run.FirstWrite != 2 {
				t.Fatalf("plan at %d, first write at %d: plan must come first", run.PlanAt, run.FirstWrite)
			}
			bil08Proposal(t, fd, run.GateIDs)
			if c.name == "injection" {
				if err := agenttest.QuotedOnly(run.Run, bilcInjected); err != nil {
					t.Fatal(err)
				}
				if bilcCarries(fd, run.GateIDs, bilcInjected) || bilcToolInputsCarry(run.Run, bilcInjected) {
					t.Fatal("injected text reached the proposal")
				}
			}
		})
	}
}

// bil08Proposal checks the run's one gate: billing.credit_note on the run's draft, prepared
// by the acting user, pending, and decidable by neither an agent principal nor the acting
// user.
func bil08Proposal(t *testing.T, fd *agenttest.FeatureDriver, gateIDs []string) {
	t.Helper()
	gs := bilcGateDecisions(t, fd, gateIDs)
	if len(gs) != 1 || gs[0].Kind != GateCreditNote || gs[0].Subject.ID != "draft-document-1" {
		t.Fatalf("gates = %+v, want one %s on draft-document-1", gs, GateCreditNote)
	}
	if d := fd.Seed.Store.Drafts; len(d) != 1 || d[0].PreparedBy != agenttest.FeatureActingFor || d[0].MatterID != agenttest.TrustMatter {
		t.Fatalf("drafts = %+v, want one credit note prepared by the acting user", d)
	}
}

func TestFBIL08_CreditNoteCents(t *testing.T) {
	tests := []struct {
		body string
		want []int64
	}{
		{"Credit $1,130.00 on SYN-2026-0003 dated 2026-09-30.", []int64{113_000}},
		{"fees $1,000.00 and tax 130.00", []int64{100_000, 13_000}},
		{"credit $56 and 1,250", []int64{5_600, 125_000}},
		{"no figures, bill 0003 of 2026-09-30", nil},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			got, err := CreditNoteCents(tt.body)
			if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("CreditNoteCents = %v (%v), want %v", got, err, tt.want)
			}
		})
	}
}

// rule: ARC-U-027
func TestFBIL08_CheckCreditNote(t *testing.T) {
	pv := agenttools.NewProvenance()
	pv.Observe("run-1", []byte(`{"total_cents":113000,"tax_cents":13000}`))
	tests := []struct {
		name string
		run  string
		body string
		want error
	}{
		{"sourced", "run-1", "Credit $1,130.00 incl. tax $130.00", nil},
		{"invented", "run-1", "Credit $1,129.99", agenttools.ErrUnsourcedValue},
		{"other run's figure", "run-2", "Credit $1,130.00", agenttools.ErrUnsourcedValue},
		{"no amount", "run-1", "Credit as agreed", agent.ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := []byte(`{"matter_id":"m","title":"t","body_markdown":"` + tt.body + `"}`)
			if err := CheckCreditNote(pv, tt.run, in); !errors.Is(err, tt.want) {
				t.Fatalf("CheckCreditNote = %v, want %v", err, tt.want)
			}
		})
	}
}

// rule: ARC-U-027
// The feature only prepares: its source reaches no module API that authorizes, approves,
// delivers or executes.
func TestFBIL08_PrepareOnly(t *testing.T) {
	if err := bilcPrepareOnly("ai_f_bil_08.go"); err != nil {
		t.Fatal(err)
	}
}

// rule: AI-R-002
func TestFBIL08_Def(t *testing.T) {
	def := DraftCreditNoteDef()
	bilcPromptLint(t, def, "Draft credit note", "draft credit note on credit-notes")
	tests := []struct {
		field     string
		got, want string
	}{
		{"source", string(def.Source), "screen-action"},
		{"trigger", def.Trigger, "on request: credit-notes > Draft credit note"},
		{"on request", strings.Join(def.Actions, ";"), "credit-notes > Draft credit note"},
		{"gate kinds", strings.Join(def.GateKinds, ","), "billing.credit_note"},
		{"screen action", strings.Join(def.Actions, ";"), ActionDraftCreditNote},
		{"cadence", string(trigger.ParseCadence(def.Trigger)), string(trigger.CadenceNone)},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}
