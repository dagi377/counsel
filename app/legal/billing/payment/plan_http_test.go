package payment_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/eventroute/eventroutetest"
	"github.com/dagi377/counsel/kernel/registry"
)

// proposedPlans is a payment.PlanStore that records proposed plans.
type proposedPlans struct {
	payment.PlanStore
	proposed []payment.Plan
}

func (m *proposedPlans) Propose(_ context.Context, _ pgx.Tx, p payment.Plan) (string, error) {
	m.proposed = append(m.proposed, p)
	return "0192a000-0000-7000-8000-0000000000e1", nil
}

func TestPlanHTTPCreate(t *testing.T) {
	const (
		matter = "0192a000-0000-7000-8000-0000000000a1"
		client = "0192a000-0000-7000-8000-0000000000c1"
		bill   = "0192a000-0000-7000-8000-0000000000b1"
	)
	body := func(schedule, second string, cents int64) string {
		b, _ := json.Marshal(map[string]any{"matter_id": matter, "contact_id": client, "bill_ids": []string{bill},
			"schedule": schedule, "start_on": "2026-11-01", "instalments": []map[string]any{
				{"due_on": "2026-11-01", "amount_cents": 5000}, {"due_on": second, "amount_cents": cents}}})
		return string(b)
	}
	tests := []struct {
		name   string
		user   string
		agent  bool
		body   string
		status int
		opened bool
	}{
		// rule: ARC-I-027
		{"opens the gate with the caller as preparer", "clerk", false, body("monthly", "2026-12-01", 5000), http.StatusAccepted, true},
		{"agent prepares for its user", "clerk", true, body("monthly", "2026-12-01", 5000), http.StatusAccepted, true},
		{"dates off the schedule refused", "clerk", false, body("weekly", "2026-12-01", 5000), http.StatusBadRequest, false},
		// rule: BIL-C-064
		{"non-positive instalment refused", "clerk", false, body("monthly", "2026-12-01", 0), http.StatusBadRequest, false},
		{"unknown field refused", "clerk", false, `{"matter_id":"x","extra":1}`, http.StatusBadRequest, false},
		// rule: ARC-I-051
		{"no capability", eventroutetest.NoFirmCap, false, body("monthly", "2026-12-01", 5000), http.StatusForbidden, false},
		// rule: ARC-I-042
		{"walled matter", eventroutetest.Walled, false, body("monthly", "2026-12-01", 5000), http.StatusNotFound, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := eventroutetest.User(tt.user)
			if tt.agent {
				p = registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: tt.user}
			}
			gates, plans := &eventroutetest.Gates{}, &proposedPlans{}
			h := &payment.PlanHTTP{Route: eventroutetest.Route(p), DB: eventroutetest.Tx{}, Gates: gates, Plans: plans}
			rec := eventroutetest.Post(h.Routes, "/payment-plans", tt.body)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
			if got := len(gates.Opened) == 1 && len(plans.proposed) == 1; got != tt.opened {
				t.Fatalf("opened = %v (gates %d, plans %d), want %v", got, len(gates.Opened), len(plans.proposed), tt.opened)
			}
			if !tt.opened {
				return
			}
			o := gates.Opened[0]
			var pl map[string]any
			_ = json.Unmarshal(o.Payload, &pl)
			var raw map[string]json.RawMessage
			_ = json.Unmarshal(o.Payload, &raw)
			if o.Kind != payment.KindPaymentPlan || !reflect.DeepEqual(o.Preparer, p) || o.Subject.Kind != payment.SubjectPaymentPlan ||
				pl["plan_id"] != o.Subject.ID || string(raw["amount_cents"]) != "10000" || pl["contact_id"] != client {
				t.Fatalf("opened %+v payload %v", o, pl)
			}
		})
	}
}

func TestPlanHTTPUnwired(t *testing.T) {
	h := &payment.PlanHTTP{Route: eventroutetest.Route(eventroutetest.User("clerk"))}
	if rec := eventroutetest.Post(h.Routes, "/payment-plans", `{}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
