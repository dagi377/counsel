package bill

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates/decider"
	"github.com/dagi377/counsel/kernel/registry"
)

// KindPrebillReview is the G-B1 gate: approval moves the matter's draft bill for the period
// to pre-bill approved, so it can go on to final approval (billing.bill_approval).
const KindPrebillReview = "billing.prebill_review"

// StatusPrebillApproved is the pre-bill state of a draft bill (billing.prebill_review, 5631).
const StatusPrebillApproved = "prebill_approved"

// PrebillDraft is the draft bill a pre-bill review covers, locked in the gate tx.
type PrebillDraft struct {
	ID         string
	MatterID   string
	Status     Status
	ApprovalID string // the pre-bill gate already recorded on it, "" before
}

// PrebillRecord is one billing.prebill_review row.
type PrebillRecord struct {
	BillID     string
	MatterID   string
	PeriodFrom string // YYYY-MM-DD or ""
	PeriodTo   string
	ApprovalID string
	ApprovedBy string
	ApprovedAt time.Time
}

// PrebillStore keeps pre-bill reviews. Every method runs in the gate tx. PGPrebill
// implements it.
type PrebillStore interface {
	// LockDraft locks the bill (billID, or else the matter's one draft whose period ends on
	// periodTo) and returns it, or ErrNotFound.
	LockDraft(ctx context.Context, tx pgx.Tx, billID, matterID, periodTo string) (PrebillDraft, error)
	// MarkPrebill writes the pre-bill approval.
	MarkPrebill(ctx context.Context, tx pgx.Tx, r PrebillRecord) error
}

type prebillPayload struct {
	MatterID   string `json:"matter_id"`
	PeriodFrom string `json:"period_from"`
	PeriodTo   string `json:"period_to"`
}

// PrebillEffect is the billing.prebill_review EffectFunc.
type PrebillEffect struct {
	Store PrebillStore
	Now   func() time.Time
}

// Apply marks the draft bill pre-bill approved. A bill this gate already marked is a no-op.
func (e *PrebillEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	by, err := decider.Check(g)
	if err != nil {
		return fmt.Errorf("pre-bill effect: %w", err)
	}
	if e == nil || e.Store == nil || e.Now == nil {
		return fmt.Errorf("pre-bill effect %s: not wired: %w", g.GateID, ErrInvalid)
	}
	var p prebillPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.MatterID == "" || !validDate(p.PeriodTo) ||
		(p.PeriodFrom != "" && (!validDate(p.PeriodFrom) || p.PeriodFrom > p.PeriodTo)) {
		return fmt.Errorf("pre-bill effect %s: payload: %w", g.GateID, ErrInvalid)
	}
	billID := ""
	if g.Subject.Kind == SubjectBill {
		billID = g.Subject.ID
	}
	d, err := e.Store.LockDraft(ctx, tx, billID, p.MatterID, p.PeriodTo)
	if err != nil {
		return fmt.Errorf("pre-bill effect %s: %w", g.GateID, err)
	}
	// rule: ARC-I-012
	// Idempotent per gate.
	if d.ApprovalID == g.GateID {
		return nil
	}
	// rule: BIL-C-014
	// Only an unapproved draft on the approved matter, reviewed once, becomes pre-bill approved.
	if d.ApprovalID != "" || d.MatterID != p.MatterID || (d.Status != StatusDraft && d.Status != StatusPendingApproval) {
		return fmt.Errorf("pre-bill effect %s: bill %s is %s: %w", g.GateID, d.ID, d.Status, ErrInvalid)
	}
	return e.Store.MarkPrebill(ctx, tx, PrebillRecord{BillID: d.ID, MatterID: p.MatterID, PeriodFrom: p.PeriodFrom,
		PeriodTo: p.PeriodTo, ApprovalID: g.GateID, ApprovedBy: by, ApprovedAt: e.Now()})
}

func validDate(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// PGPrebill is PrebillStore over billing.bill and billing.prebill_review.
type PGPrebill struct{}

var _ PrebillStore = PGPrebill{}

// lockPrebillDraftSQL locks the named bill, or the matter's single draft whose period ends on
// period_to (a daterange's upper bound is exclusive).
const lockPrebillDraftSQL = `SELECT b.id::text, b.matter_id::text, b.status, coalesce(r.approval_id::text, '')
FROM billing.bill b LEFT JOIN billing.prebill_review r ON r.bill_id = b.id
WHERE CASE WHEN $1 <> '' THEN b.id = NULLIF($1, '')::uuid
  ELSE b.matter_id = $2::uuid AND b.status IN ('draft','pending_approval') AND upper(b.period) = $3::date + 1 END
FOR UPDATE OF b`

const insertPrebillSQL = `INSERT INTO billing.prebill_review (bill_id, matter_id, period_from, period_to, approval_id, approved_by, approved_at)
VALUES ($1::uuid, $2::uuid, NULLIF($3, '')::date, $4::date, $5::uuid, $6::uuid, $7)`

// LockDraft implements PrebillStore. More than one matching draft is ErrInvalid.
func (PGPrebill) LockDraft(ctx context.Context, tx pgx.Tx, billID, matterID, periodTo string) (PrebillDraft, error) {
	rows, err := tx.Query(ctx, lockPrebillDraftSQL, billID, matterID, periodTo)
	if err != nil {
		return PrebillDraft{}, fmt.Errorf("pre-bill draft: %w", err)
	}
	defer rows.Close()
	var out []PrebillDraft
	for rows.Next() {
		var d PrebillDraft
		if err := rows.Scan(&d.ID, &d.MatterID, &d.Status, &d.ApprovalID); err != nil {
			return PrebillDraft{}, fmt.Errorf("pre-bill draft: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return PrebillDraft{}, fmt.Errorf("pre-bill draft: %w", err)
	}
	switch len(out) {
	case 0:
		return PrebillDraft{}, fmt.Errorf("pre-bill draft on matter %s: %w", matterID, ErrNotFound)
	case 1:
		return out[0], nil
	}
	return PrebillDraft{}, fmt.Errorf("pre-bill draft on matter %s: %d drafts: %w", matterID, len(out), ErrInvalid)
}

// MarkPrebill implements PrebillStore.
func (PGPrebill) MarkPrebill(ctx context.Context, tx pgx.Tx, r PrebillRecord) error {
	if _, err := tx.Exec(ctx, insertPrebillSQL, r.BillID, r.MatterID, r.PeriodFrom, r.PeriodTo, r.ApprovalID, r.ApprovedBy, r.ApprovedAt); err != nil {
		return fmt.Errorf("pre-bill %s: %w", r.BillID, err)
	}
	return nil
}
