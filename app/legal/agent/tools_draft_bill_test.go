package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/billing/batch"
	"github.com/dagi377/counsel/app/legal/billing/draft"
	"github.com/dagi377/counsel/app/legal/billing/draft/draftfake"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	draftBillMatter = "91111111-1111-4111-8111-111111111111"
	draftBillWalled = "92222222-2222-4222-8222-222222222222"
	billEntryA      = "93333333-3333-4333-8333-333333333333"
	billEntryB      = "94444444-4444-4444-8444-444444444444"
	billEntryX      = "95555555-5555-4555-8555-555555555555"
	draftBillRun    = "run-bill"
	billNoPlan      = "run-bill-noplan"
	billCapName     = "billing.draft"
	billApprove     = "billing.approve"
)

// wipDrafts is a batch.Drafts over draftfake: Create assembles a draft from the matter's
// unbilled WIP, one fee line per approved entry, and keeps the bill in draft.
type wipDrafts struct {
	store *draftfake.Fake

	mu     sync.Mutex
	keys   map[string]string
	status map[string]string // bill id -> draft | final | issued
}

func (w *wipDrafts) Create(ctx context.Context, key string, t batch.Target, _ clock.Date) (string, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if id, ok := w.keys[key]; ok {
		return id, false, nil
	}
	id := fmt.Sprintf("96666666-6666-4666-8666-%012d", len(w.keys)+1)
	lines := []draft.Line{
		{ID: "l1", Kind: draft.LineFee, SortOrder: 1, Description: "Synthetic review", AmountCents: 50000, SourceTimeEntryID: billEntryA},
		{ID: "l2", Kind: draft.LineFee, SortOrder: 2, Description: "Synthetic call", AmountCents: 20000, SourceTimeEntryID: billEntryB},
	}
	if err := w.store.SaveLines(ctx, id, lines); err != nil {
		return "", false, err
	}
	w.keys[key] = id
	w.status[id] = "draft"
	return id, true, nil
}

type draftBillWorld struct {
	reg    *agenttools.Registry
	drafts *wipDrafts
	store  *draftfake.Fake
	gates  *gatefake.Fake
	opened *int // gates opened through the tool's opener
}

// newDraftBillWorld registers the real draft_bill on a Registry over module fakes. draftBillRun has a
// plan and read the figure 5000; billNoPlan has no plan. clerk holds actorCaps and is walled
// from draftBillWalled.
func newDraftBillWorld(t *testing.T, actorCaps map[string]bool) draftBillWorld {
	t.Helper()
	ctx := context.Background()
	walls := rolesfake.New()
	walls.Wall(clerk, draftBillWalled)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	store := draftfake.New()
	w := draftBillWorld{
		store:  store,
		drafts: &wipDrafts{store: store, keys: map[string]string{}, status: map[string]string{}},
		gates:  gatefake.New(func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }, gatefake.Caps{"partner": {billApprove}}),
	}
	err := w.gates.Register(gate.GateKind{
		Slug: agent.GatePrebillReview, Approver: billApprove, Risk: gate.RiskHigh, MinApprovers: 1, TTL: 168 * time.Hour,
		Effect: func(context.Context, pgx.Tx, gate.Approved) error { return nil },
	})
	if err != nil {
		t.Fatalf("gate kind: %v", err)
	}
	w.opened = new(int)
	open := agenttools.GateOpener(func(ctx context.Context, kind string, s registry.Subject, payload json.RawMessage, by agenttools.Principal) (gate.GateID, error) {
		*w.opened++
		return w.gates.Open(ctx, nil, kind, s, payload, by)
	})
	pv := agenttools.NewProvenance()
	pv.Observe(draftBillRun, json.RawMessage(`{"unbilled_cents":5000}`))
	var drafts batch.Drafts = w.drafts
	var lines draft.Store = store
	tool, err := agent.DraftBillHandler().Build(agent.NewDeps(can, walls, drafts, lines, open, pv))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, draftBillRun, []string{"draft the bill"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	w.reg = agenttools.NewRegistry(plans)
	if err := w.reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return w
}

func billInput(matterID string, actions ...string) string {
	in := `{"matter_id":"` + matterID + `","period_from":"2026-09-01","period_to":"2026-09-30"`
	if len(actions) > 0 {
		in += `,"line_actions":[`
		for i, a := range actions {
			if i > 0 {
				in += ","
			}
			in += a
		}
		in += `]`
	}
	return in + `}`
}

func billScope(run string, kinds ...string) context.Context {
	return agenttools.WithScope(context.Background(), agenttools.NewScope(run, kinds))
}

// rule: ARC-I-035
// rule: ARC-I-036
// rule: ARC-I-039
func TestARC_I_035_DraftBill(t *testing.T) {
	caps := map[string]bool{billCapName: true}
	grant := []string{billCapName}
	agentP := func(run string, g []string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: clerk, Grant: g}
	}
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}
	inScope := billScope(draftBillRun, agent.GatePrebillReview)
	writeDown := func(cents int) string {
		return fmt.Sprintf(`{"source_id":"%s","action":"write_down","write_down_cents":%d,"narrative":"Synthetic duplicate"}`, billEntryA, cents)
	}
	hold := `{"source_id":"` + billEntryB + `","action":"hold"}`
	cases := []struct {
		name      string
		ctx       context.Context
		p         registry.Principal
		actorCaps map[string]bool
		in        string
		wantErr   error
		wantNets  map[string]int64 // source entry -> net cents on the draft
	}{
		{"agent assembles the pre-bill", inScope, agentP(draftBillRun, grant), caps, billInput(draftBillMatter), nil, map[string]int64{billEntryA: 50000, billEntryB: 20000}},
		{"sourced write-down and a hold", inScope, agentP(draftBillRun, grant), caps, billInput(draftBillMatter, writeDown(5000), hold), nil, map[string]int64{billEntryA: 45000}},
		{"no charge", inScope, agentP(draftBillRun, grant), caps, billInput(draftBillMatter, `{"source_id":"`+billEntryB+`","action":"no_charge"}`), nil, map[string]int64{billEntryA: 50000, billEntryB: 0}},
		{"manual path", context.Background(), user, caps, billInput(draftBillMatter), nil, map[string]int64{billEntryA: 50000, billEntryB: 20000}},
		{"walled matter", inScope, agentP(draftBillRun, grant), caps, billInput(draftBillWalled), agent.ErrNotVisible, nil},
		{"walled matter on the manual path", context.Background(), user, caps, billInput(draftBillWalled), agent.ErrNotVisible, nil},
		{"run without the capability", inScope, agentP(draftBillRun, nil), caps, billInput(draftBillMatter), agenttools.ErrMissingCapability, nil},
		{"acting user without the capability", inScope, agentP(draftBillRun, grant), map[string]bool{}, billInput(draftBillMatter), agent.ErrForbidden, nil},
		{"no report_plan in the run", billScope(billNoPlan, agent.GatePrebillReview), agentP(billNoPlan, grant), caps, billInput(draftBillMatter), agenttools.ErrPlanRequired, nil},
		{"gate kind outside the run scope", billScope(draftBillRun, "billing.payment_allocation"), agentP(draftBillRun, grant), caps, billInput(draftBillMatter), agenttools.ErrGateKindNotAllowed, nil},
		{"invented write-down amount", inScope, agentP(draftBillRun, grant), caps, billInput(draftBillMatter, writeDown(4999)), agenttools.ErrUnsourcedValue, nil},
		{"write-down without amount", inScope, agentP(draftBillRun, grant), caps, billInput(draftBillMatter, `{"source_id":"`+billEntryA+`","action":"write_down"}`), agent.ErrInvalidInput, nil},
		{"source not on the draft", inScope, agentP(draftBillRun, grant), caps, billInput(draftBillMatter, `{"source_id":"`+billEntryX+`","action":"hold"}`), agent.ErrInvalidInput, nil},
		{"period ends before it starts", inScope, agentP(draftBillRun, grant), caps, `{"matter_id":"` + draftBillMatter + `","period_from":"2026-09-30","period_to":"2026-09-01"}`, agent.ErrInvalidInput, nil},
		{"no field to finalize", inScope, agentP(draftBillRun, grant), caps, `{"matter_id":"` + draftBillMatter + `","period_to":"2026-09-30","issue":true}`, agenttools.ErrInvalidInput, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newDraftBillWorld(t, tc.actorCaps)
			res, err := w.reg.Call(tc.ctx, tc.p, agent.ToolDraftBill, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				if *w.opened != 0 {
					t.Fatalf("refused call opened a gate")
				}
				return
			}
			// rule: ARC-I-035
			if err := agenttools.CheckWriteResult(res); err != nil {
				t.Fatalf("write result: %v", err)
			}
			g, err := w.gates.Get(tc.ctx, gate.GateID(res.GateID))
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			var payload struct {
				MatterID string `json:"matter_id"`
				PeriodTo string `json:"period_to"`
				BillID   string `json:"bill_id"`
			}
			if err := json.Unmarshal(g.Payload, &payload); err != nil {
				t.Fatalf("payload: %v", err)
			}
			if g.Kind != agent.GatePrebillReview || g.Status != gate.StatusPending || g.Subject.ID != payload.BillID ||
				payload.MatterID != draftBillMatter || payload.PeriodTo != "2026-09-30" {
				t.Fatalf("gate = %+v, payload %+v", g, payload)
			}
			// rule: ARC-I-035
			// Never final, never issued: the bill is still a draft.
			if st := w.drafts.status[payload.BillID]; st != "draft" {
				t.Fatalf("bill status = %q, want draft", st)
			}
			lines, err := w.store.Lines(tc.ctx, payload.BillID)
			if err != nil {
				t.Fatalf("lines: %v", err)
			}
			got := map[string]int64{}
			for _, l := range lines {
				got[l.SourceTimeEntryID] = w.store.Net(l)
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.wantNets) {
				t.Fatalf("nets = %v, want %v", got, tc.wantNets)
			}
		})
	}
}

// rule: ARC-I-039
// The pre-bill gate is never decided by an agent principal: it stays pending.
func TestARC_I_039_DraftBillGateNotDecidedByAgent(t *testing.T) {
	cases := []struct {
		name string
		by   registry.Principal
	}{
		{"agent run approves its pre-bill", registry.Principal{Kind: registry.PrincipalAgent, RunID: draftBillRun, ActingFor: clerk, Grant: []string{billCapName, billApprove}}},
		{"another agent run approves", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-other", ActingFor: "partner", Grant: []string{billApprove}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := billScope(draftBillRun, agent.GatePrebillReview)
			w := newDraftBillWorld(t, map[string]bool{billCapName: true})
			p := registry.Principal{Kind: registry.PrincipalAgent, RunID: draftBillRun, ActingFor: clerk, Grant: []string{billCapName}}
			res, err := w.reg.Call(ctx, p, agent.ToolDraftBill, json.RawMessage(billInput(draftBillMatter)))
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			g0, err := w.gates.Get(ctx, gate.GateID(res.GateID))
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			d := gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: g0.PayloadHash, MFAVerifiedAt: time.Date(2026, 10, 11, 8, 59, 0, 0, time.UTC)}
			err = agenttools.DecideGuard{Engine: w.gates}.Decide(ctx, nil, g0.ID, g0.Version, d, tc.by)
			g, gerr := w.gates.Get(ctx, g0.ID)
			if gerr != nil {
				t.Fatalf("gate: %v", gerr)
			}
			if !errors.Is(err, agenttools.ErrAgentDecides) || g.Status != gate.StatusPending {
				t.Fatalf("decide err = %v status %s, want ErrAgentDecides and pending", err, g.Status)
			}
		})
	}
}
