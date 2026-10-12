package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates/decider"
	"github.com/dagi377/counsel/kernel/registry"
)

// KindPaymentPlan is the G-B9 gate: approval activates a proposed payment plan.
const KindPaymentPlan = "billing.payment_plan"

// SubjectPaymentPlan is the gate subject kind of a billing.payment_plan row.
const SubjectPaymentPlan = "billing.payment_plan"

// ErrPlanTerms means a plan's instalments, bills or client differ from the approved terms.
var ErrPlanTerms = errors.New("payment: plan does not match the approved terms")

// Instalment is one dated instalment of a plan (billing.payment_plan.instalments).
type Instalment struct {
	DueOn       string `json:"due_on"` // YYYY-MM-DD, America/Toronto
	AmountCents int64  `json:"amount_cents"`
}

// Plan is a billing.payment_plan row.
type Plan struct {
	ID          string
	ClientID    string
	Instalments []Instalment
	BillIDs     []string
	Status      string // proposed, active, completed, defaulted, cancelled
	ApprovalID  string
}

// PlanBill is a bill a plan covers, as the effect checks it.
type PlanBill struct {
	MatterID  string
	PayerID   string
	Status    string
	OpenCents int64
}

// PlanStore keeps payment plans. Every method runs in the caller's tx. PGPlans implements it.
type PlanStore interface {
	// Propose records a proposed plan and returns its id.
	Propose(ctx context.Context, tx pgx.Tx, p Plan) (string, error)
	// LockPlan locks plan id and returns it, or ErrNotFound.
	LockPlan(ctx context.Context, tx pgx.Tx, id string) (Plan, error)
	// Bills reads the plan's bills by id; a missing bill is absent from the map.
	Bills(ctx context.Context, tx pgx.Tx, ids []string) (map[string]PlanBill, error)
	// Activate moves the proposed plan to active under gate gateID.
	Activate(ctx context.Context, tx pgx.Tx, id, gateID string) error
}

// planPayload is the billing.payment_plan gate payload. amount_cents is the plan total; the
// plan is named by plan_id or by the gate subject.
type planPayload struct {
	PlanID      string `json:"plan_id"`
	MatterID    string `json:"matter_id"`
	ContactID   string `json:"contact_id"`
	AmountCents int64  `json:"amount_cents"`
	Schedule    string `json:"schedule"`
	StartOn     string `json:"start_on"`
}

// planStep is the gap between instalments per schedule.
var planStep = map[string]func(time.Time) time.Time{
	"weekly":   func(d time.Time) time.Time { return d.AddDate(0, 0, 7) },
	"biweekly": func(d time.Time) time.Time { return d.AddDate(0, 0, 14) },
	"monthly":  func(d time.Time) time.Time { return d.AddDate(0, 1, 0) },
}

// PlanEffect is the billing.payment_plan EffectFunc: it activates the proposed plan whose
// instalments, bills and client match the approved terms.
type PlanEffect struct {
	Store PlanStore
}

// Apply activates the plan. A plan this gate already activated is a no-op.
func (e *PlanEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	if _, err := decider.Check(g); err != nil {
		return fmt.Errorf("plan effect: %w", err)
	}
	if e == nil || e.Store == nil {
		return fmt.Errorf("plan effect %s: not wired: %w", g.GateID, ErrInvalid)
	}
	var p planPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil {
		return fmt.Errorf("plan effect %s: payload: %w", g.GateID, ErrInvalid)
	}
	id := p.PlanID
	if g.Subject.Kind == SubjectPaymentPlan {
		if id != "" && id != g.Subject.ID {
			return fmt.Errorf("plan effect %s: subject is plan %s: %w", g.GateID, g.Subject.ID, ErrInvalid)
		}
		id = g.Subject.ID
	}
	step, ok := planStep[p.Schedule]
	start, err := time.Parse(time.DateOnly, p.StartOn)
	if id == "" || !ok || err != nil || p.ContactID == "" || p.MatterID == "" || p.AmountCents <= 0 {
		return fmt.Errorf("plan effect %s: plan, client, matter, schedule, start and amount are required: %w", g.GateID, ErrInvalid)
	}
	plan, err := e.Store.LockPlan(ctx, tx, id)
	if err != nil {
		return fmt.Errorf("plan effect %s: %w", g.GateID, err)
	}
	// rule: ARC-I-012
	// Idempotent per gate.
	if plan.Status == "active" && plan.ApprovalID == g.GateID {
		return nil
	}
	if plan.Status != "proposed" {
		return fmt.Errorf("plan effect %s: plan is %s: %w", g.GateID, plan.Status, ErrInvalid)
	}
	if err := checkPlan(plan, p, start, step); err != nil {
		return fmt.Errorf("plan effect %s: %w", g.GateID, err)
	}
	bills, err := e.Store.Bills(ctx, tx, plan.BillIDs)
	if err != nil {
		return fmt.Errorf("plan effect %s: %w", g.GateID, err)
	}
	var open int64
	for _, id := range plan.BillIDs {
		b, ok := bills[id]
		if !ok {
			return fmt.Errorf("plan effect %s: bill %s: %w", g.GateID, id, ErrNotFound)
		}
		if b.MatterID != p.MatterID || b.PayerID != p.ContactID || (b.Status != "delivered" && b.Status != "partially_paid") {
			return fmt.Errorf("plan effect %s: bill %s: %w", g.GateID, id, ErrPlanTerms)
		}
		open += b.OpenCents
	}
	// rule: DM-047
	// A plan never schedules more than is open on its bills.
	if p.AmountCents > open {
		return fmt.Errorf("plan effect %s: %d cents over %d open: %w", g.GateID, p.AmountCents, open, ErrPlanTerms)
	}
	return e.Store.Activate(ctx, tx, id, g.GateID)
}

// checkPlan matches the stored plan to the approved terms: the client, the integer-cent total
// and the dates, which start on start_on and step by the schedule.
func checkPlan(plan Plan, p planPayload, start time.Time, step func(time.Time) time.Time) error {
	if plan.ClientID != p.ContactID || len(plan.Instalments) == 0 || len(plan.BillIDs) == 0 {
		return ErrPlanTerms
	}
	var sum int64
	due := start
	for _, in := range plan.Instalments {
		d, err := time.Parse(time.DateOnly, in.DueOn)
		if err != nil || !d.Equal(due) || in.AmountCents <= 0 {
			return ErrPlanTerms
		}
		sum += in.AmountCents
		due = step(due)
	}
	if sum != p.AmountCents {
		return ErrPlanTerms
	}
	return nil
}

// PGPlans is PlanStore over billing.payment_plan.
type PGPlans struct{}

var _ PlanStore = PGPlans{}

const insertPlanSQL = `INSERT INTO billing.payment_plan (client_id, instalments, bill_ids)
VALUES ($1::uuid, $2::jsonb, $3::uuid[]) RETURNING id::text`

const lockPlanSQL = `SELECT id::text, client_id::text, instalments, bill_ids::text[], status, coalesce(approval_id::text, '')
FROM billing.payment_plan WHERE id = $1::uuid FOR UPDATE`

const planBillsSQL = `SELECT b.id::text, b.matter_id::text, b.payer_id::text, b.status, bb.balance_cents - bb.reserved_cents
FROM billing.bill b JOIN billing.bill_balance bb ON bb.bill_id = b.id WHERE b.id = ANY($1::uuid[]) FOR UPDATE OF b`

const activatePlanSQL = `UPDATE billing.payment_plan SET status = 'active', approval_id = $2::uuid
WHERE id = $1::uuid AND status = 'proposed'`

// Propose implements PlanStore.
func (PGPlans) Propose(ctx context.Context, tx pgx.Tx, p Plan) (string, error) {
	if p.ClientID == "" || len(p.Instalments) == 0 || len(p.BillIDs) == 0 {
		return "", ErrInvalid
	}
	raw, err := json.Marshal(p.Instalments)
	if err != nil {
		return "", fmt.Errorf("propose plan: %w", err)
	}
	var id string
	if err := tx.QueryRow(ctx, insertPlanSQL, p.ClientID, raw, p.BillIDs).Scan(&id); err != nil {
		return "", fmt.Errorf("propose plan: %w", err)
	}
	return id, nil
}

// LockPlan implements PlanStore.
func (PGPlans) LockPlan(ctx context.Context, tx pgx.Tx, id string) (Plan, error) {
	var p Plan
	var raw []byte
	err := tx.QueryRow(ctx, lockPlanSQL, id).Scan(&p.ID, &p.ClientID, &raw, &p.BillIDs, &p.Status, &p.ApprovalID)
	if missing(err) {
		return Plan{}, fmt.Errorf("plan %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Plan{}, fmt.Errorf("lock plan %s: %w", id, err)
	}
	if err := json.Unmarshal(raw, &p.Instalments); err != nil {
		return Plan{}, fmt.Errorf("plan %s instalments: %w", id, err)
	}
	return p, nil
}

// Bills implements PlanStore.
func (PGPlans) Bills(ctx context.Context, tx pgx.Tx, ids []string) (map[string]PlanBill, error) {
	rows, err := tx.Query(ctx, planBillsSQL, ids)
	if err != nil {
		return nil, fmt.Errorf("plan bills: %w", err)
	}
	defer rows.Close()
	out := map[string]PlanBill{}
	for rows.Next() {
		var id string
		var b PlanBill
		if err := rows.Scan(&id, &b.MatterID, &b.PayerID, &b.Status, &b.OpenCents); err != nil {
			return nil, fmt.Errorf("plan bills: %w", err)
		}
		out[id] = b
	}
	return out, rows.Err()
}

// Activate implements PlanStore.
func (PGPlans) Activate(ctx context.Context, tx pgx.Tx, id, gateID string) error {
	tag, err := tx.Exec(ctx, activatePlanSQL, id, gateID)
	if err != nil {
		return fmt.Errorf("activate plan %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("activate plan %s: %w", id, ErrInvalid)
	}
	return nil
}
