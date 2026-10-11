package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	payMatter   = "a1111111-1111-4111-8111-111111111111"
	payWalled   = "a2222222-2222-4222-8222-222222222222"
	payOK       = "a3333333-3333-4333-8333-333333333333" // 10000 on payMatter
	payOnWalled = "a4444444-4444-4444-8444-444444444444" // on payWalled
	payNoMatter = "a5555555-5555-4555-8555-555555555555" // no matter on the capture
	payReversed = "a6666666-6666-4666-8666-666666666666"
	payUnknown  = "a7777777-7777-4777-8777-777777777777"
	payBill     = "a8888888-8888-4888-8888-888888888888"
	payRun      = "run-pay"
	payNoPlan   = "run-pay-noplan"
	payCapName  = "billing.payments"
)

// payments is a payment.Service whose Get serves fixed synthetic payments; the tool reads
// nothing else.
type payments struct {
	payment.Service
	byID map[string]payment.Payment
}

func (s payments) Get(_ context.Context, id string) (payment.Payment, error) {
	p, ok := s.byID[id]
	if !ok {
		return payment.Payment{}, fmt.Errorf("payment %s: %w", id, payment.ErrNotFound)
	}
	return p, nil
}

type payWorld struct {
	reg    *agenttools.Registry
	gates  *gatefake.Fake
	opened *int
}

// newPayWorld registers the real propose_payment_allocation on a Registry over fakes. payRun
// has a plan and read the figures 10000, 6000 and 4000; payNoPlan has no plan. clerk holds
// actorCaps and is walled from payWalled.
func newPayWorld(t *testing.T, actorCaps map[string]bool) payWorld {
	t.Helper()
	ctx := context.Background()
	walls := rolesfake.New()
	walls.Wall(clerk, payWalled)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	capture := func(matter string, cents int64) payment.Capture {
		return payment.Capture{ClientID: "client-1", BillID: payBill, Kind: payment.KindBillPayment, AmountCents: cents, MatterID: matter}
	}
	var svc payment.Service = payments{byID: map[string]payment.Payment{
		payOK:       {ID: payOK, Capture: capture(payMatter, 10000), Status: payment.StatusCaptured},
		payOnWalled: {ID: payOnWalled, Capture: capture(payWalled, 10000), Status: payment.StatusCaptured},
		payNoMatter: {ID: payNoMatter, Capture: capture("", 10000), Status: payment.StatusCaptured},
		payReversed: {ID: payReversed, Capture: capture(payMatter, 10000), Status: payment.StatusReversed, Reason: payment.ReasonNSF},
	}}
	w := payWorld{
		gates:  gatefake.New(func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }, gatefake.Caps{clerk: {payCapName}}),
		opened: new(int),
	}
	err := w.gates.Register(gate.GateKind{
		Slug: agent.GatePaymentAllocation, Approver: payCapName, Risk: gate.RiskHigh, MinApprovers: 1, TTL: 72 * time.Hour,
		Effect: func(context.Context, pgx.Tx, gate.Approved) error { return nil },
	})
	if err != nil {
		t.Fatalf("gate kind: %v", err)
	}
	open := agenttools.GateOpener(func(ctx context.Context, kind string, s registry.Subject, payload json.RawMessage, by agenttools.Principal) (gate.GateID, error) {
		*w.opened++
		return w.gates.Open(ctx, nil, kind, s, payload, by)
	})
	pv := agenttools.NewProvenance()
	pv.Observe(payRun, json.RawMessage(`{"amount_cents":10000,"bills":[{"open_cents":6000},{"open_cents":4000}]}`))
	tool, err := agent.ProposePaymentAllocationHandler().Build(agent.NewDeps(can, walls, svc, open, pv))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, payRun, []string{"allocate the payment"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	w.reg = agenttools.NewRegistry(plans)
	if err := w.reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return w
}

func payInput(paymentID string, allocs ...string) string {
	in := `{"payment_id":"` + paymentID + `","allocations":[`
	for i, a := range allocs {
		if i > 0 {
			in += ","
		}
		in += a
	}
	return in + `]}`
}

func toBill(cents int) string {
	return fmt.Sprintf(`{"target_type":"bill","target_id":"%s","amount_cents":%d}`, payBill, cents)
}

func unapplied(cents int) string {
	return fmt.Sprintf(`{"target_type":"unapplied","amount_cents":%d}`, cents)
}

func payScope(run string, kinds ...string) context.Context {
	return agenttools.WithScope(context.Background(), agenttools.NewScope(run, kinds))
}

// rule: ARC-I-035
// rule: ARC-I-036
// rule: ARC-I-039
func TestARC_I_035_ProposePaymentAllocation(t *testing.T) {
	caps := map[string]bool{payCapName: true}
	grant := []string{payCapName}
	agentP := func(run string, g []string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: clerk, Grant: g}
	}
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}
	inScope := payScope(payRun, agent.GatePaymentAllocation)
	cases := []struct {
		name      string
		ctx       context.Context
		p         registry.Principal
		actorCaps map[string]bool
		in        string
		wantErr   error
		wantN     int
	}{
		{"whole payment to one bill", inScope, agentP(payRun, grant), caps, payInput(payOK, toBill(10000)), nil, 1},
		{"split under the payment", inScope, agentP(payRun, grant), caps, payInput(payOK, toBill(6000), unapplied(4000)), nil, 2},
		{"manual path", context.Background(), user, caps, payInput(payOK, toBill(7000)), nil, 1},
		{"sum over the payment", inScope, agentP(payRun, grant), caps, payInput(payOK, toBill(6000), toBill(6000)), agent.ErrInvalidInput, 0},
		{"sum over the payment on the manual path", context.Background(), user, caps, payInput(payOK, toBill(10001)), agent.ErrInvalidInput, 0},
		{"invented amount", inScope, agentP(payRun, grant), caps, payInput(payOK, toBill(5999)), agenttools.ErrUnsourcedValue, 0},
		{"one invented amount refuses the split", inScope, agentP(payRun, grant), caps, payInput(payOK, toBill(6000), unapplied(3999)), agenttools.ErrUnsourcedValue, 0},
		{"walled matter", inScope, agentP(payRun, grant), caps, payInput(payOnWalled, toBill(6000)), agent.ErrNotVisible, 0},
		{"payment with no matter", inScope, agentP(payRun, grant), caps, payInput(payNoMatter, toBill(6000)), agent.ErrNotVisible, 0},
		{"unknown payment", inScope, agentP(payRun, grant), caps, payInput(payUnknown, toBill(6000)), payment.ErrNotFound, 0},
		{"reversed payment", inScope, agentP(payRun, grant), caps, payInput(payReversed, toBill(6000)), payment.ErrReversed, 0},
		{"bill without its id", inScope, agentP(payRun, grant), caps, payInput(payOK, `{"target_type":"bill","amount_cents":6000}`), agent.ErrInvalidInput, 0},
		{"run without the capability", inScope, agentP(payRun, nil), caps, payInput(payOK, toBill(6000)), agenttools.ErrMissingCapability, 0},
		{"acting user without the capability", inScope, agentP(payRun, grant), map[string]bool{}, payInput(payOK, toBill(6000)), agent.ErrForbidden, 0},
		{"no report_plan in the run", payScope(payNoPlan, agent.GatePaymentAllocation), agentP(payNoPlan, grant), caps, payInput(payOK, toBill(6000)), agenttools.ErrPlanRequired, 0},
		{"gate kind outside the run scope", payScope(payRun, "billing.prebill_review"), agentP(payRun, grant), caps, payInput(payOK, toBill(6000)), agenttools.ErrGateKindNotAllowed, 0},
		{"no allocations", inScope, agentP(payRun, grant), caps, payInput(payOK), agenttools.ErrInvalidInput, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newPayWorld(t, tc.actorCaps)
			res, err := w.reg.Call(tc.ctx, tc.p, agent.ToolProposePaymentAllocation, json.RawMessage(tc.in))
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
				PaymentID   string `json:"payment_id"`
				Allocations []struct {
					AmountCents int64 `json:"amount_cents"`
				} `json:"allocations"`
			}
			if err := json.Unmarshal(g.Payload, &payload); err != nil {
				t.Fatalf("payload: %v", err)
			}
			if g.Kind != agent.GatePaymentAllocation || g.Status != gate.StatusPending || g.Subject.ID != payOK ||
				payload.PaymentID != payOK || len(payload.Allocations) != tc.wantN {
				t.Fatalf("gate = %+v, payload %+v", g, payload)
			}
		})
	}
}

// rule: ARC-I-039
// The allocation gate is never decided by an agent principal: it stays pending.
func TestARC_I_039_PaymentAllocationGateNotDecidedByAgent(t *testing.T) {
	cases := []struct {
		name string
		by   registry.Principal
	}{
		{"agent run approves its allocation", registry.Principal{Kind: registry.PrincipalAgent, RunID: payRun, ActingFor: clerk, Grant: []string{payCapName}}},
		{"another agent run approves", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-other", ActingFor: clerk, Grant: []string{payCapName}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := payScope(payRun, agent.GatePaymentAllocation)
			w := newPayWorld(t, map[string]bool{payCapName: true})
			p := registry.Principal{Kind: registry.PrincipalAgent, RunID: payRun, ActingFor: clerk, Grant: []string{payCapName}}
			res, err := w.reg.Call(ctx, p, agent.ToolProposePaymentAllocation, json.RawMessage(payInput(payOK, toBill(10000))))
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
