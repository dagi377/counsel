package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/eventroute"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// maxInstalments bounds a plan's schedule (ten years of monthly instalments).
const maxInstalments = 120

// maxPlanBills bounds the bills one plan covers.
const maxPlanBills = 50

// PlanHTTP serves POST /payment-plans: it records a proposed billing.payment_plan and opens
// billing.payment_plan with the acting user as preparer. Only the gate's effect activates it.
type PlanHTTP struct {
	Route eventroute.Route
	DB    eventroute.TxRunner
	Gates eventroute.Gates
	Plans PlanStore
	// MinApprovers is the billing.payment_plan approver count for the Gate body.
	MinApprovers int
}

func init() { add(registerPlanHTTP) }

// registerPlanHTTP mounts POST /payment-plans. Boot wiring supplies the dependencies; until it does, the
// route answers 503.
func registerPlanHTTP(r registry.Registry) error {
	r.Routes((&PlanHTTP{}).Routes)
	return nil
}

// Routes mounts the route.
func (h *PlanHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// The route writes only the proposed plan; activating it is billing.payment_plan's.
	r.Method(http.MethodPost, "/payment-plans", server.Declare(h.create))
}

type planBody struct {
	MatterID    string       `json:"matter_id"`
	ContactID   string       `json:"contact_id"`
	BillIDs     []string     `json:"bill_ids"`
	Schedule    string       `json:"schedule"`
	StartOn     string       `json:"start_on"`
	Instalments []Instalment `json:"instalments"`
}

// terms checks the body and returns the plan and the gate payload, or false.
func (b planBody) terms() (Plan, planPayload, bool) {
	ids := map[string]bool{}
	ok := idPattern.MatchString(b.MatterID) && idPattern.MatchString(b.ContactID) &&
		len(b.BillIDs) > 0 && len(b.BillIDs) <= maxPlanBills && len(b.Instalments) > 0 && len(b.Instalments) <= maxInstalments
	for _, id := range b.BillIDs {
		ok = ok && idPattern.MatchString(id) && !ids[id]
		ids[id] = true
	}
	step, known := planStep[b.Schedule]
	start, err := time.Parse(time.DateOnly, b.StartOn)
	if !ok || !known || err != nil {
		return Plan{}, planPayload{}, false
	}
	// rule: BIL-C-064
	// Integer cents: every instalment is positive and the total stays in range.
	var total int64
	for _, in := range b.Instalments {
		if in.AmountCents <= 0 || in.AmountCents > maxCents {
			return Plan{}, planPayload{}, false
		}
		total += in.AmountCents
	}
	if total > maxCents {
		return Plan{}, planPayload{}, false
	}
	plan := Plan{ClientID: b.ContactID, Instalments: b.Instalments, BillIDs: b.BillIDs, Status: "proposed"}
	p := planPayload{MatterID: b.MatterID, ContactID: b.ContactID, AmountCents: total, Schedule: b.Schedule, StartOn: b.StartOn}
	// The dates start on start_on and step by the schedule, as the effect re-checks them.
	if checkPlan(plan, p, start, step) != nil {
		return Plan{}, planPayload{}, false
	}
	return plan, p, true
}

func (h *PlanHTTP) create(w http.ResponseWriter, r *http.Request) {
	ready := h.DB != nil && h.Gates != nil && h.Plans != nil
	h.Route.Serve(w, r, ready, CapPayments, func(w http.ResponseWriter, r *http.Request, p registry.Principal) {
		var b planBody
		if eventroute.Decode(r, &b) != nil {
			h.Route.Invalid(w, r, "Body is not valid")
			return
		}
		plan, payload, ok := b.terms()
		if !ok {
			h.Route.Invalid(w, r, "Body is not valid")
			return
		}
		// rule: ARC-I-042
		// A walled matter answers 404 before anything is written.
		if !h.Route.CanOnMatter(w, r, p, CapPayments, b.MatterID) {
			return
		}
		h.Route.OpenGate(w, r, h.DB, h.Gates, p, KindPaymentPlan, h.MinApprovers,
			func(ctx context.Context, tx pgx.Tx) (eventroute.Prepared, error) {
				id, err := h.Plans.Propose(ctx, tx, plan)
				if err != nil {
					return eventroute.Prepared{}, err
				}
				payload.PlanID = id
				raw, err := json.Marshal(payload)
				if err != nil {
					return eventroute.Prepared{}, err
				}
				return eventroute.Prepared{Subject: gate.Subject{Kind: SubjectPaymentPlan, ID: id}, Payload: raw}, nil
			})
	})
}
