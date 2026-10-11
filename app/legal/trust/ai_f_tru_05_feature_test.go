package trust_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// truFloor is the synthetic retainer floor agreed in the engagement, in cents.
const truFloor = 300_000

// truEmailDraft is the id the SeedStore gives the run's one email draft.
const truEmailDraft = "draft-email-1"

// truTopUp is a run below the floor: the ledger's signed balance and the request the
// script sends to the client.
type truTopUp struct {
	balance int64
	body    string
}

// truTopUps are the two ledgers below the floor: one in funds, one overdrawn. Every amount is
// one get_trust_balance, get_bill or the shortfall returned.
var truTopUps = map[string]truTopUp{
	"below the floor": {120_000, "Your trust balance is 120000 cents, below the agreed 300000 cents. " +
		"Please deposit 180000 cents. Bill total 113000 cents."},
	// rule: BIL-L-036
	"overdrawn": {truOverdrawn, "Your trust account is overdrawn: the balance is -5000 cents, below the agreed 300000 cents. " +
		"Please deposit 305000 cents."},
}

// truTopUpCalls is the F-TRU-05 script over w: plan, reads, then one top-up request to to,
// submitted for approval.
func truTopUpCalls(w truWorld, to, body string) []truCall {
	return []truCall{
		truPlan("read", "email"),
		{agent.ToolGetMatter, map[string]any{"matter_id": w.matter}},
		{agent.ToolGetTrustBalance, map[string]any{"matter_id": w.matter}},
		{agent.ToolGetBill, map[string]any{"bill_id": w.bill}},
		{agent.ToolDraftEmail, map[string]any{"matter_id": w.matter, "to_contact_ids": []string{to},
			"subject": "Trust retainer top-up request", "body_markdown": body, "submit": true}},
	}
}

// truTopUpRun fires F-TRU-05 once for w's matter at balance. observe records the
// shortfall's figures into the run's provenance, as the event handler does.
func truTopUpRun(t *testing.T, w truWorld, balance int64, calls []truCall, runID string, observe bool) agenttest.FeatureRun {
	t.Helper()
	w.env.ledgers[w.matter] = balance
	s, err := trust.NewRetainerShortfall(w.matter, balance, truFloor)
	if err != nil {
		t.Fatalf("shortfall: %v", err)
	}
	if observe {
		if err := s.Observe(w.env.provenance, runID); err != nil {
			t.Fatalf("observe: %v", err)
		}
	}
	model := truScript(t, trust.FeatureRetainerReplenishment, calls, "Drafted a retainer top-up request for approval.")
	return w.fd.Fire(context.Background(), model, runID, s.Prompt())
}

// rule: AI-R-003
// rule: ARC-I-035
func TestFTRU05_DefMatchesSpec(t *testing.T) {
	truSpecSync(t, trust.RetainerReplenishmentDef(), false)
	// The restated kind is G-OUT, the one draft_email opens.
	if trust.GateOutgoingEmail != agent.GateOutgoingEmail {
		t.Fatalf("gate kind = %s, want %s", trust.GateOutgoingEmail, agent.GateOutgoingEmail)
	}
}

// rule: AI-R-005
func TestFTRU05_Prompt(t *testing.T) {
	truPrompt(t, trust.RetainerReplenishmentDef(), "draft_email", "integer cents", "Never hide an overdraft")
}

// F-TRU-05 starts when a ledger falls below its floor, acting for the event's user.
func TestFTRU05_TriggerBinding(t *testing.T) {
	b, err := trust.RetainerReplenishmentBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"feature", b.FeatureID, trust.FeatureRetainerReplenishment},
		{"events", strings.Join(b.Events, ","), trust.EventLedgerBelowFloor},
		{"no schedule", b.Cadence, trigger.CadenceNone},
		{"acts for the event user", b.ActingFor, ""},
		{"no screen action", len(trust.RetainerReplenishmentDef().Actions), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// rule: BIL-L-036
// rule: AI-R-003
func TestRetainerShortfall(t *testing.T) {
	tests := []struct {
		name          string
		balance       int64
		floor         int64
		wantErr       error
		wantOverdrawn bool
		wantShortfall int64
	}{
		{"below the floor", 120_000, truFloor, nil, false, 180_000},
		{"empty ledger", 0, truFloor, nil, false, truFloor},
		{"overdrawn", -5_000, truFloor, nil, true, 305_000},
		{"at the floor", truFloor, truFloor, trust.ErrInvalid, false, 0},
		{"above the floor", truFloor + 1, truFloor, trust.ErrInvalid, false, 0},
		{"no floor", -5_000, 0, trust.ErrInvalid, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := trust.NewRetainerShortfall("matter-1", tt.balance, tt.floor)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if s.BalanceCents != tt.balance || s.Overdrawn != tt.wantOverdrawn || s.ShortfallCents != tt.wantShortfall {
				t.Fatalf("shortfall = %+v", s)
			}
			// The prompt states the signed balance and the overdraft.
			for _, w := range []string{fmt.Sprintf("balance_cents=%d ", tt.balance), fmt.Sprintf("overdrawn=%t ", tt.wantOverdrawn)} {
				if !strings.Contains(s.Prompt(), w) {
					t.Fatalf("prompt %q lacks %q", s.Prompt(), w)
				}
			}
		})
	}
	s, _ := trust.NewRetainerShortfall("matter-1", -5_000, truFloor)
	if err := s.Observe(nil, "run-1"); !errors.Is(err, trust.ErrPortMissing) {
		t.Fatalf("Observe without provenance = %v, want ErrPortMissing", err)
	}
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFTRU05_FromFeature(t *testing.T) {
	type tcase struct {
		name      string
		topUp     truTopUp
		narrative string
	}
	cases := []tcase{
		{"below the floor", truTopUps["below the floor"], "Prepared the mediation brief."},
		{"overdrawn", truTopUps["overdrawn"], "Prepared the mediation brief."},
		{"injection", truTopUps["overdrawn"], truInjected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := truFeature(t, trust.RetainerReplenishmentDef(), tc.narrative)
			// rule: ARC-I-035
			if len(w.fd.StandIns) != 0 {
				t.Fatalf("stand-ins = %v, want none", w.fd.StandIns)
			}
			calls := truTopUpCalls(w, w.client, tc.topUp.body)
			run := truTopUpRun(t, w, tc.topUp.balance, calls, "run-f-tru-05-"+strings.ReplaceAll(tc.name, " ", "-"), true)
			if run.Err != nil || len(run.Refused) != 0 || len(run.StandIns) != 0 {
				t.Fatalf("run err = %v, refused = %v, stand-ins = %v", run.Err, run.Refused, run.StandIns)
			}
			// rule: ARC-I-038
			// The run makes exactly the scripted calls: injected text adds none.
			if want := truToolNames(calls); !slices.Equal(run.Tools, want) {
				t.Fatalf("tools = %v, want %v", run.Tools, want)
			}
			// rule: ARC-I-039
			if run.PlanAt != 0 || run.FirstWrite != slices.Index(run.Tools, agent.ToolDraftEmail) {
				t.Fatalf("plan at %d, first write %d: report_plan must precede draft_email", run.PlanAt, run.FirstWrite)
			}
			if d := w.fd.Seed.Store.Drafts; len(d) != 1 || d[0].ID != truEmailDraft || d[0].PreparedBy != agenttest.FeatureActingFor {
				t.Fatalf("drafts = %+v, want one request prepared by the acting user", d)
			}
			if len(run.GateIDs) != 1 {
				t.Fatalf("gates = %v, want one %s", run.GateIDs, trust.GateOutgoingEmail)
			}
			truCheckGate(t, w.fd, run.GateIDs[0], trust.GateOutgoingEmail)
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
func TestFTRU05_FromFeatureRefusesWrites(t *testing.T) {
	over := truTopUps["overdrawn"]
	tests := []struct {
		name    string
		body    string
		other   bool // address the client of another matter
		observe bool
		extra   []truCall
		refused string
		wantErr error
		drafts  int
		gates   int
	}{
		{name: "gate kind outside the Def", body: over.body, observe: true, extra: []truCall{truSubmit(truEmailDraft, "trust.withdrawal")},
			refused: agent.ToolSubmitForApproval, wantErr: agenttools.ErrGateKindNotAllowed, drafts: 1, gates: 1},
		{name: "model-invented amount", body: strings.Replace(over.body, "305000", "310000", 1), observe: true,
			refused: agent.ToolDraftEmail, wantErr: agenttools.ErrUnsourcedValue},
		{name: "model-computed shortfall", body: over.body, observe: false,
			refused: agent.ToolDraftEmail, wantErr: agenttools.ErrUnsourcedValue},
		// rule: BIL-L-036
		{name: "overdraft without its sign", body: strings.Replace(over.body, "-5000 cents", "5000 cents", 1), observe: true,
			refused: agent.ToolDraftEmail, wantErr: agenttools.ErrUnsourcedValue},
		{name: "amount in dollars", body: strings.Replace(over.body, "305000 cents", "$3,050.00", 1), observe: true,
			refused: agent.ToolDraftEmail, wantErr: agenttools.ErrUnsourcedValue},
		{name: "recipient not a matter contact", body: over.body, other: true, observe: true,
			refused: agent.ToolDraftEmail, wantErr: agent.ErrNotMatterContact},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := truFeature(t, trust.RetainerReplenishmentDef(), "Prepared the mediation brief.")
			to := w.client
			if tt.other {
				to = w.overdrwnClient
			}
			calls := append(truTopUpCalls(w, to, tt.body), tt.extra...)
			run := truTopUpRun(t, w, over.balance, calls, "run-f-tru-05-refused", tt.observe)
			if r, ok := truRefused(run, tt.refused); !ok || !errors.Is(r.Err, tt.wantErr) {
				t.Fatalf("refused = %+v, want %s refused with %v", run.Refused, tt.refused, tt.wantErr)
			}
			// No proposal row beyond the request itself.
			if len(run.GateIDs) != tt.gates || len(w.fd.Seed.Store.Drafts) != tt.drafts {
				t.Fatalf("gates = %v, drafts = %d; want %d and %d", run.GateIDs, len(w.fd.Seed.Store.Drafts), tt.gates, tt.drafts)
			}
		})
	}
}

// rule: AI-R-003
func TestFTRU05_PrepareOnly(t *testing.T) {
	truPrepareOnly(t, "ai_f_tru_05.go")
}
