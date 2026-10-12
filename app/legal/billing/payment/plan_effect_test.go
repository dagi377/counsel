package payment_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/gates/decider"
	"github.com/dagi377/counsel/kernel/registry"
)

// memPlans is an in-memory payment.PlanStore over one plan.
type memPlans struct {
	plan      payment.Plan
	bills     map[string]payment.PlanBill
	activated int
}

func (m *memPlans) Propose(context.Context, pgx.Tx, payment.Plan) (string, error) {
	return m.plan.ID, nil
}

func (m *memPlans) LockPlan(_ context.Context, _ pgx.Tx, id string) (payment.Plan, error) {
	if id != m.plan.ID {
		return payment.Plan{}, payment.ErrNotFound
	}
	return m.plan, nil
}

func (m *memPlans) Bills(context.Context, pgx.Tx, []string) (map[string]payment.PlanBill, error) {
	return m.bills, nil
}

func (m *memPlans) Activate(_ context.Context, _ pgx.Tx, _, gateID string) error {
	m.plan.Status, m.plan.ApprovalID = "active", gateID
	m.activated++
	return nil
}

func TestPlanEffect(t *testing.T) {
	const (
		planID = "0192a000-0000-7000-8000-0000000000e1"
		matter = "0192a000-0000-7000-8000-0000000000e2"
		client = "0192a000-0000-7000-8000-0000000000e3"
		billA  = "0192a000-0000-7000-8000-0000000000e4"
	)
	user := func(id string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalUser, UserID: id}
	}
	payload := func(amount int64, schedule string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"plan_id": planID, "matter_id": matter, "contact_id": client,
			"amount_cents": amount, "schedule": schedule, "start_on": "2026-11-01"})
		return b
	}
	tests := []struct {
		name      string
		payload   json.RawMessage
		approvers []registry.Principal
		repeat    bool
		want      int
		err       error
	}{
		{"monthly plan activated", payload(30000, "monthly"), []registry.Principal{user("lawyer")}, false, 1, nil},
		// rule: ARC-I-012
		{"repeat is a no-op", payload(30000, "monthly"), []registry.Principal{user("lawyer")}, true, 1, nil},
		{"total differs refused", payload(29999, "monthly"), []registry.Principal{user("lawyer")}, false, 0, payment.ErrPlanTerms},
		{"schedule differs refused", payload(30000, "weekly"), []registry.Principal{user("lawyer")}, false, 0, payment.ErrPlanTerms},
		// rule: ARC-I-019
		{"agent approver refused", payload(30000, "monthly"), []registry.Principal{{Kind: registry.PrincipalAgent}}, false, 0, decider.ErrAgentApproves},
		// rule: ARC-I-027
		{"preparer refused", payload(30000, "monthly"), []registry.Principal{user("clerk")}, false, 0, decider.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &memPlans{
				plan: payment.Plan{ID: planID, ClientID: client, Status: "proposed", BillIDs: []string{billA},
					Instalments: []payment.Instalment{{DueOn: "2026-11-01", AmountCents: 10000}, {DueOn: "2026-12-01", AmountCents: 10000}, {DueOn: "2027-01-01", AmountCents: 10000}}},
				bills: map[string]payment.PlanBill{billA: {MatterID: matter, PayerID: client, Status: "delivered", OpenCents: 30000}},
			}
			e := &payment.PlanEffect{Store: st}
			g := registry.Approved{GateID: "gate-1", Kind: payment.KindPaymentPlan, Payload: tt.payload, Preparer: user("clerk"), Approvers: tt.approvers}
			err := e.Apply(context.Background(), nil, g)
			if tt.repeat && err == nil {
				err = e.Apply(context.Background(), nil, g)
			}
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("Apply = %v, want %v", err, tt.err)
			}
			if st.activated != tt.want {
				t.Fatalf("activated %d times, want %d", st.activated, tt.want)
			}
		})
	}
}
