package bill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

// GateBillApproval is the gate (G-B2) whose effect is the only way a bill becomes approved.
const GateBillApproval = "billing.bill_approval"

// ErrAgentApproves is returned when the deciding principal of a bill approval is not a user.
var ErrAgentApproves = errors.New("bill: an agent may not approve")

// ApprovalPayload is the billing.bill_approval gate payload (spec/gates.json).
type ApprovalPayload struct {
	BillID         string `json:"bill_id"`
	MatterID       string `json:"matter_id"`
	TotalCents     int64  `json:"total_cents"`
	DeliveryMethod string `json:"delivery_method"`
}

// ApprovalStore reads and writes bills inside the gate's tx.
type ApprovalStore interface {
	// Lock reads the bill FOR UPDATE, or returns ErrNotFound.
	Lock(ctx context.Context, tx pgx.Tx, id ID) (Bill, error)
	// SaveApproval writes the number, status, bill date and approval fields of b.
	SaveApproval(ctx context.Context, tx pgx.Tx, b Bill) error
}

// FeesBookWriter appends a fees book row inside the gate's tx. Rows are never changed or
// deleted.
type FeesBookWriter interface {
	Append(ctx context.Context, tx pgx.Tx, e FeesBookEntry) error
}

// ApproveEffect is the billing.bill_approval effect: in the gate's tx it takes the next
// gapless number, approves the bill, writes its fees book row and posts its accrual entry
// (BIL-J-003). Any error rolls the gate tx back, so a failed approval leaves no number, no
// register row and no journal entry.
type ApproveEffect struct {
	Store    ApprovalStore
	FeesBook FeesBookWriter
	Journal  JournalSource    // the bill's lines and tax breakdown
	Ledger   Poster           // the GL ledger the approval entry is posted to
	Prefix   string           // number series prefix, e.g. "INV"
	Now      func() time.Time // the approval instant
	// Next issues the number; nil means NextNumber.
	Next func(ctx context.Context, tx pgx.Tx, prefix string, year int) (string, error)
}

// Effects returns the effect handler for gates.RegisterKinds.
func Effects(e *ApproveEffect) map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{GateBillApproval: e.Apply}
}

// Apply is the billing.bill_approval EffectFunc.
func (e *ApproveEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	if g.Kind != GateBillApproval {
		return fmt.Errorf("bill approval gate %s kind %q: %w", g.GateID, g.Kind, ErrInvalid)
	}
	var p ApprovalPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.BillID == "" {
		return fmt.Errorf("bill approval gate %s payload: %w", g.GateID, ErrInvalid)
	}
	if len(g.Approvers) == 0 {
		return fmt.Errorf("bill approval gate %s: no approver: %w", g.GateID, ErrInvalid)
	}
	if e.Journal == nil || e.Ledger == nil {
		return fmt.Errorf("bill approval gate %s: no ledger: %w", g.GateID, ErrInvalid)
	}
	// The agent never approves; an agent's acting-for user counts as the preparer.
	approver := g.Approvers[len(g.Approvers)-1]
	if approver.Kind != registry.PrincipalUser || approver.UserID == "" {
		return fmt.Errorf("bill approval gate %s: %w", g.GateID, ErrAgentApproves)
	}
	preparer := g.Preparer.UserID
	if g.Preparer.Kind == registry.PrincipalAgent {
		preparer = g.Preparer.ActingFor
	}
	if approver.UserID == preparer {
		return fmt.Errorf("bill approval gate %s: %w", g.GateID, ErrPreparerApproves)
	}
	b, err := e.Store.Lock(ctx, tx, ID(p.BillID))
	if err != nil {
		return fmt.Errorf("bill approval gate %s: %w", g.GateID, err)
	}
	if b.ApprovalID == g.GateID && b.Status != StatusPendingApproval {
		return nil // replay: already applied
	}
	// The gate approves the bill it was opened on, at the total it showed.
	if p.MatterID != b.MatterID || p.TotalCents != b.TotalCents {
		return fmt.Errorf("bill approval gate %s: payload does not match bill %s: %w", g.GateID, b.ID, ErrInvalid)
	}
	at := e.Now()
	next := e.Next
	if next == nil {
		next = NextNumber
	}
	// rule: BIL-C-120
	// The number comes from the sequence row in this same tx.
	number, err := next(ctx, tx, e.Prefix, businessDate(at).Year())
	if err != nil {
		return fmt.Errorf("bill approval gate %s: %w", g.GateID, err)
	}
	approved, err := applyApproval(b, Approval{BillID: b.ID, ApprovalID: g.GateID, ApprovedBy: approver.UserID,
		ApprovedAt: at, Prefix: e.Prefix}, number)
	if err != nil {
		return fmt.Errorf("bill approval gate %s: %w", g.GateID, err)
	}
	if err := e.Store.SaveApproval(ctx, tx, approved); err != nil {
		return fmt.Errorf("bill approval gate %s: %w", g.GateID, err)
	}
	row, err := NewFeesBookEntry(approved)
	if err != nil {
		return fmt.Errorf("bill approval gate %s: %w", g.GateID, err)
	}
	if err := e.FeesBook.Append(ctx, tx, row); err != nil {
		return fmt.Errorf("bill approval gate %s: fees book: %w", g.GateID, err)
	}
	lines, taxes, err := e.Journal.JournalLines(ctx, tx, approved.ID)
	if err != nil {
		return fmt.Errorf("bill approval gate %s: %w", g.GateID, err)
	}
	approved.Lines = lines
	// rule: BIL-J-003
	// Accrual: the receivable, revenue and tax payable are recognised at approval, in the
	// same tx, dated the bill date (the tax point).
	entry, err := ApprovalJournal(approved, taxes)
	if err != nil {
		return fmt.Errorf("bill approval gate %s: %w", g.GateID, err)
	}
	if _, err := e.Ledger.Post(ctx, tx, entry); err != nil {
		return fmt.Errorf("bill approval gate %s: journal: %w", g.GateID, err)
	}
	return nil
}

// PGStore is the Postgres ApprovalStore over billing.bill. Lines are not loaded: approval
// does not change them.
type PGStore struct{}

var _ ApprovalStore = PGStore{}

// Lock reads the bill FOR UPDATE.
func (PGStore) Lock(ctx context.Context, tx pgx.Tx, id ID) (Bill, error) {
	var b Bill
	var issue *time.Time
	var approvedAt *time.Time
	err := tx.QueryRow(ctx, `
SELECT id::text, coalesce(number, ''), matter_id::text, payer_id::text, status, coalesce(created_by::text, ''),
       issue_date, fees_cents, taxable_disb_cents, nontaxable_disb_cents, discount_cents, tax_cents, total_cents,
       coalesce(place_of_supply_province, ''), coalesce(approval_id::text, ''), coalesce(approved_by::text, ''), approved_at
  FROM billing.bill WHERE id = $1::uuid FOR UPDATE`, string(id)).Scan(
		&b.ID, &b.Number, &b.MatterID, &b.PayerID, &b.Status, &b.PreparedBy,
		&issue, &b.FeesCents, &b.TaxableDisbCents, &b.NontaxableDisbCents, &b.DiscountCents, &b.TaxCents, &b.TotalCents,
		&b.PlaceOfSupplyProvince, &b.ApprovalID, &b.ApprovedBy, &approvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Bill{}, fmt.Errorf("lock bill %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Bill{}, fmt.Errorf("lock bill %s: %w", id, err)
	}
	if issue != nil {
		y, m, d := issue.Date()
		b.IssueDate = time.Date(y, m, d, 0, 0, 0, 0, business)
	}
	if approvedAt != nil {
		b.ApprovedAt = *approvedAt
	}
	return b, nil
}

// SaveApproval moves a pending bill to approved. The database checks the approved
// billing.bill_approval gate (bill_guard) and separation of duties (bill_sod).
func (PGStore) SaveApproval(ctx context.Context, tx pgx.Tx, b Bill) error {
	tag, err := tx.Exec(ctx, `
UPDATE billing.bill
   SET status = $2, number = $3, issue_date = $4::date, approval_id = $5::uuid, approved_by = $6::uuid,
       approved_at = $7, updated_by = $6::uuid
 WHERE id = $1::uuid AND status = 'pending_approval'`,
		string(b.ID), string(b.Status), b.Number, b.IssueDate.In(business).Format(time.DateOnly),
		b.ApprovalID, b.ApprovedBy, b.ApprovedAt)
	if err != nil {
		return fmt.Errorf("save approval of bill %s: %w", b.ID, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("save approval of bill %s: %w", b.ID, ErrNotPending)
	}
	return nil
}
