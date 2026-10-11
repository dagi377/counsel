package credit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/money"
)

// WriteOffBuckets is a write-off split by bucket: fees, taxable disbursements, non-taxable
// disbursements, sales tax and interest, as billing.write_off stores it. All int64 cents.
type WriteOffBuckets struct {
	Fee      int64
	Dtx      int64
	Dnt      int64
	Tax      int64
	Interest int64
}

// Sum is the total of the five buckets.
func (b WriteOffBuckets) Sum() int64 { return b.Fee + b.Dtx + b.Dnt + b.Tax + b.Interest }

func (b WriteOffBuckets) slice() []int64 { return []int64{b.Fee, b.Dtx, b.Dnt, b.Tax, b.Interest} }

// SplitWriteOff splits amount over the bill's outstanding buckets in proportion to them, by
// largest remainder (money.Allocate), and verifies the result: the parts sum to amount and no
// part exceeds its outstanding bucket. It fails closed on anything else.
func SplitWriteOff(amount int64, open WriteOffBuckets) (WriteOffBuckets, error) {
	if amount <= 0 {
		return WriteOffBuckets{}, fmt.Errorf("split write-off: %w", ErrInvalidAmount)
	}
	weights := open.slice()
	for _, w := range weights {
		if w < 0 {
			return WriteOffBuckets{}, fmt.Errorf("split write-off: negative outstanding bucket: %w", ErrInvalidState)
		}
	}
	if amount > open.Sum() {
		return WriteOffBuckets{}, fmt.Errorf("split write-off: %w", ErrExceedsBalance)
	}
	parts, err := money.Allocate(money.Cents(amount), weights)
	if err != nil || len(parts) != len(weights) {
		return WriteOffBuckets{}, fmt.Errorf("split write-off: allocate: %w", ErrInvalidState)
	}
	out := WriteOffBuckets{Fee: int64(parts[0]), Dtx: int64(parts[1]), Dnt: int64(parts[2]), Tax: int64(parts[3]), Interest: int64(parts[4])}
	got := out.slice()
	for i := range got {
		if got[i] < 0 || got[i] > weights[i] {
			return WriteOffBuckets{}, fmt.Errorf("split write-off: bucket exceeds outstanding: %w", ErrInvalidState)
		}
	}
	if out.Sum() != amount {
		return WriteOffBuckets{}, fmt.Errorf("split write-off: parts do not sum: %w", ErrInvalidState)
	}
	return out, nil
}

// ResponsibleLawyers is the matter port the billing module reads a bill's responsible lawyer
// through; billing never joins matter.matter. It runs in the caller's tx.
type ResponsibleLawyers interface {
	// ResponsibleLawyer returns the responsible lawyer of matterID, or an error.
	ResponsibleLawyer(ctx context.Context, tx pgx.Tx, matterID string) (string, error)
}

// PGWriteOffs keeps write-offs in Postgres. A draft lives in billing.write_off_draft with its
// bucket split; SaveApproved, which only the G-B5 effects call, marks it approved and writes
// the approved row to billing.write_off. It implements WriteOffStore and WriteOffBills.
type PGWriteOffs struct {
	// Lawyers reads the responsible lawyer of a bill's matter. Nil fails closed.
	Lawyers ResponsibleLawyers
}

var (
	_ WriteOffStore = PGWriteOffs{}
	_ WriteOffBills = PGWriteOffs{}
)

// notFound maps a missing row or a malformed id to ErrNotFound.
func notFound(err error) bool {
	var pg *pgconn.PgError
	return errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02")
}

const writeOffLockSQL = `SELECT id::text, bill_id::text, amount_cents, reason, status, prepared_by::text,
  coalesce(approved_by::text, ''), approved_at, interest_cents
  FROM billing.write_off_draft WHERE id = $1::uuid FOR UPDATE`

// Lock implements WriteOffStore: it reads draft id FOR UPDATE, with its interest bucket.
func (PGWriteOffs) Lock(ctx context.Context, tx pgx.Tx, id string) (WriteOff, error) {
	if id == "" {
		return WriteOff{}, fmt.Errorf("lock write-off: %w", ErrNotFound)
	}
	var (
		w      WriteOff
		status string
		at     *time.Time
	)
	err := tx.QueryRow(ctx, writeOffLockSQL, id).Scan(&w.ID, &w.BillID, &w.AmountCents, &w.Reason, &status,
		&w.PreparedBy, &w.ApprovedBy, &at, &w.InterestCents)
	if notFound(err) {
		return WriteOff{}, fmt.Errorf("lock write-off %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return WriteOff{}, fmt.Errorf("lock write-off %s: %w", id, err)
	}
	w.Status = Status(status)
	switch {
	case w.Status == StatusDraft && w.ApprovedBy == "" && at == nil:
	case w.Status == StatusApproved && w.ApprovedBy != "" && at != nil:
		t := *at
		w.ApprovedAt = &t
	default:
		return WriteOff{}, fmt.Errorf("lock write-off %s: status %q: %w", id, status, ErrInvalidState)
	}
	return w, nil
}

const writeOffMarkSecondSQL = `UPDATE billing.write_off_draft SET second_gate_id = $2::uuid
  WHERE id = $1::uuid AND status = 'draft' AND (second_gate_id IS NULL OR second_gate_id = $2::uuid)`

// MarkAwaitingSecond implements WriteOffStore. It sets the second gate once; a draft that is
// approved or already awaits another gate is refused.
func (PGWriteOffs) MarkAwaitingSecond(ctx context.Context, tx pgx.Tx, id, gateID string) error {
	if id == "" || gateID == "" {
		return fmt.Errorf("mark write-off awaiting second approval: %w", ErrInvalidState)
	}
	tag, err := tx.Exec(ctx, writeOffMarkSecondSQL, id, gateID)
	if err != nil {
		return fmt.Errorf("mark write-off %s awaiting second approval: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("mark write-off %s awaiting second approval: %w", id, ErrInvalidState)
	}
	return nil
}

const writeOffAwaitingSQL = `SELECT coalesce(second_gate_id::text, '') FROM billing.write_off_draft WHERE id = $1::uuid`

// AwaitingSecond implements WriteOffStore.
func (PGWriteOffs) AwaitingSecond(ctx context.Context, tx pgx.Tx, id string) (string, error) {
	var gateID string
	err := tx.QueryRow(ctx, writeOffAwaitingSQL, id).Scan(&gateID)
	if notFound(err) {
		return "", fmt.Errorf("write-off %s second approval: %w", id, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("write-off %s second approval: %w", id, err)
	}
	return gateID, nil
}

const writeOffApprovalReadSQL = `SELECT bill_id::text, amount_cents, status, prepared_by::text, coalesce(approved_by::text, ''),
  coalesce(credit_gate_id::text, ''), fee_cents, dtx_cents, dnt_cents, tax_cents, interest_cents, reason
  FROM billing.write_off_draft WHERE id = $1::uuid FOR UPDATE`

const writeOffApproveSQL = `UPDATE billing.write_off_draft SET status = 'approved', approved_by = $2::uuid, approved_at = $3
  WHERE id = $1::uuid AND status = 'draft'`

const writeOffInsertSQL = `INSERT INTO billing.write_off
  (id, bill_id, amount_cents, fee_cents, dtx_cents, dnt_cents, tax_cents, interest_cents, reason, approval_id)
  VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10::uuid)
  ON CONFLICT (id) DO NOTHING`

// SaveApproved implements WriteOffStore. It marks draft w.ID approved by w.ApprovedBy and
// inserts the approved row into billing.write_off with the draft's id, its bucket split and
// the G-B5 gate as approval_id. A second call for the same approval changes nothing; any
// mismatch with the stored draft is refused and nothing is written.
func (PGWriteOffs) SaveApproved(ctx context.Context, tx pgx.Tx, w WriteOff) error {
	if w.ID == "" || w.Status != StatusApproved || w.ApprovedBy == "" || w.ApprovedAt == nil || w.ApprovedAt.IsZero() {
		return fmt.Errorf("save approved write-off: %w", ErrInvalidState)
	}
	if w.ApprovedBy == w.PreparedBy {
		return fmt.Errorf("save approved write-off %s: %w", w.ID, ErrSelfApproval)
	}
	var (
		billID, status, preparer, approver, gateID, reason string
		amount                                             int64
		b                                                  WriteOffBuckets
	)
	err := tx.QueryRow(ctx, writeOffApprovalReadSQL, w.ID).Scan(&billID, &amount, &status, &preparer, &approver, &gateID,
		&b.Fee, &b.Dtx, &b.Dnt, &b.Tax, &b.Interest, &reason)
	if notFound(err) {
		return fmt.Errorf("save approved write-off %s: %w", w.ID, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("save approved write-off %s: %w", w.ID, err)
	}
	switch {
	case billID != w.BillID || amount != w.AmountCents || preparer != w.PreparedBy:
		return fmt.Errorf("save approved write-off %s: does not match the draft: %w", w.ID, ErrInvalidState)
	case gateID == "":
		return fmt.Errorf("save approved write-off %s: no G-B5 gate: %w", w.ID, ErrInvalidState)
	case b.Sum() != amount:
		return fmt.Errorf("save approved write-off %s: split does not sum: %w", w.ID, ErrInvalidState)
	case preparer == w.ApprovedBy:
		return fmt.Errorf("save approved write-off %s: %w", w.ID, ErrSelfApproval)
	}
	switch Status(status) {
	case StatusDraft:
		tag, err := tx.Exec(ctx, writeOffApproveSQL, w.ID, w.ApprovedBy, *w.ApprovedAt)
		if err != nil {
			return fmt.Errorf("save approved write-off %s: %w", w.ID, err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("save approved write-off %s: %w", w.ID, ErrInvalidState)
		}
	case StatusApproved:
		if approver != w.ApprovedBy {
			return fmt.Errorf("save approved write-off %s: approved by someone else: %w", w.ID, ErrInvalidState)
		}
	default:
		return fmt.Errorf("save approved write-off %s: status %q: %w", w.ID, status, ErrInvalidState)
	}
	if _, err := tx.Exec(ctx, writeOffInsertSQL, w.ID, billID, amount, b.Fee, b.Dtx, b.Dnt, b.Tax, b.Interest, reason, gateID); err != nil {
		return fmt.Errorf("save approved write-off %s: %w", w.ID, err)
	}
	return nil
}

// writeOffOutstandingSQL locks bill $1 and reads its outstanding buckets: the issued
// components (the discount off fees first, then taxable disbursements) and posted interest,
// less every allocation and approved write-off by bucket and every issued credit note (pre-tax
// off fees, tax off tax), plus the bill balance net of reserved fee transfers. Every source
// is a billing table.
const writeOffOutstandingSQL = `SELECT
  (b.fees_cents - LEAST(b.discount_cents, b.fees_cents) - x.fee - x.cn_fee)::bigint,
  (b.taxable_disb_cents - GREATEST(b.discount_cents - b.fees_cents, 0) - x.dtx)::bigint,
  (b.nontaxable_disb_cents - x.dnt)::bigint,
  (b.tax_cents - x.tax - x.cn_tax)::bigint,
  (x.interest_posted - x.interest)::bigint,
  (bb.balance_cents - bb.reserved_cents)::bigint
  FROM billing.bill b
  JOIN billing.bill_balance bb ON bb.bill_id = b.id
  CROSS JOIN LATERAL (SELECT
    coalesce((SELECT sum(a.fee_cents) FROM billing.payment_allocation a WHERE a.bill_id = b.id), 0)
      + coalesce((SELECT sum(w.fee_cents) FROM billing.write_off w WHERE w.bill_id = b.id), 0) AS fee,
    coalesce((SELECT sum(a.dtx_cents) FROM billing.payment_allocation a WHERE a.bill_id = b.id), 0)
      + coalesce((SELECT sum(w.dtx_cents) FROM billing.write_off w WHERE w.bill_id = b.id), 0) AS dtx,
    coalesce((SELECT sum(a.dnt_cents) FROM billing.payment_allocation a WHERE a.bill_id = b.id), 0)
      + coalesce((SELECT sum(w.dnt_cents) FROM billing.write_off w WHERE w.bill_id = b.id), 0) AS dnt,
    coalesce((SELECT sum(a.tax_cents) FROM billing.payment_allocation a WHERE a.bill_id = b.id), 0)
      + coalesce((SELECT sum(w.tax_cents) FROM billing.write_off w WHERE w.bill_id = b.id), 0) AS tax,
    coalesce((SELECT sum(a.interest_cents) FROM billing.payment_allocation a WHERE a.bill_id = b.id), 0)
      + coalesce((SELECT sum(w.interest_cents) FROM billing.write_off w WHERE w.bill_id = b.id), 0) AS interest,
    coalesce((SELECT sum(i.amount_cents) FROM billing.interest_charge i WHERE i.bill_id = b.id), 0) AS interest_posted,
    coalesce((SELECT sum(c.amount_cents) FROM billing.credit_note c
      WHERE c.bill_id = b.id AND c.status IN ('issued','refund_pending','closed')), 0) AS cn_fee,
    coalesce((SELECT sum(c.tax_cents) FROM billing.credit_note c
      WHERE c.bill_id = b.id AND c.status IN ('issued','refund_pending','closed')), 0) AS cn_tax) x
  WHERE b.id = $1::uuid
  FOR UPDATE OF b`

const writeOffDraftInsertSQL = `INSERT INTO billing.write_off_draft
  (bill_id, amount_cents, fee_cents, dtx_cents, dnt_cents, tax_cents, interest_cents, reason, prepared_by)
  VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9::uuid)
  RETURNING id::text`

// Draft stores w as a draft write-off on its bill, prepared by w.PreparedBy. The bucket split
// is computed here from the bill's outstanding buckets, under a lock on the bill; callers
// cannot set it. The outstanding buckets must add up to the bill balance net of reserved fee
// transfers, so a reduction this split can not attribute to a bucket fails closed.
func (PGWriteOffs) Draft(ctx context.Context, tx pgx.Tx, w WriteOff) (WriteOff, error) {
	if w.BillID == "" || w.PreparedBy == "" || w.Reason == "" {
		return WriteOff{}, fmt.Errorf("draft write-off: %w", ErrInvalidState)
	}
	if w.AmountCents <= 0 {
		return WriteOff{}, fmt.Errorf("draft write-off: %w", ErrInvalidAmount)
	}
	var (
		open    WriteOffBuckets
		balance int64
	)
	err := tx.QueryRow(ctx, writeOffOutstandingSQL, w.BillID).Scan(&open.Fee, &open.Dtx, &open.Dnt, &open.Tax, &open.Interest, &balance)
	if notFound(err) {
		return WriteOff{}, fmt.Errorf("draft write-off on bill %s: %w", w.BillID, ErrNotFound)
	}
	if err != nil {
		return WriteOff{}, fmt.Errorf("draft write-off on bill %s: %w", w.BillID, err)
	}
	if open.Sum() != balance {
		return WriteOff{}, fmt.Errorf("draft write-off on bill %s: buckets do not match the balance: %w", w.BillID, ErrInvalidState)
	}
	split, err := SplitWriteOff(w.AmountCents, open)
	if err != nil {
		return WriteOff{}, fmt.Errorf("draft write-off on bill %s: %w", w.BillID, err)
	}
	var id string
	if err := tx.QueryRow(ctx, writeOffDraftInsertSQL, w.BillID, w.AmountCents, split.Fee, split.Dtx, split.Dnt, split.Tax,
		split.Interest, w.Reason, w.PreparedBy).Scan(&id); err != nil {
		return WriteOff{}, fmt.Errorf("draft write-off on bill %s: %w", w.BillID, err)
	}
	return WriteOff{ID: id, BillID: w.BillID, AmountCents: w.AmountCents, Reason: w.Reason, Status: StatusDraft,
		PreparedBy: w.PreparedBy}, nil
}

const writeOffSubmitSQL = `UPDATE billing.write_off_draft SET credit_gate_id = $2::uuid
  WHERE id = $1::uuid AND status = 'draft' AND (credit_gate_id IS NULL OR credit_gate_id = $2::uuid)`

// Submit records the G-B5 gate opened for draft id, once. SaveApproved writes it to
// billing.write_off as approval_id, and refuses a draft that has none.
func (PGWriteOffs) Submit(ctx context.Context, tx pgx.Tx, id, gateID string) error {
	if id == "" || gateID == "" {
		return fmt.Errorf("submit write-off: %w", ErrInvalidState)
	}
	tag, err := tx.Exec(ctx, writeOffSubmitSQL, id, gateID)
	if err != nil {
		return fmt.Errorf("submit write-off %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("submit write-off %s: %w", id, ErrInvalidState)
	}
	return nil
}

const writeOffBillSQL = `SELECT b.id::text, b.matter_id::text, b.payer_id::text,
  b.delivered_at IS NOT NULL AND b.status <> 'void', b.tax_cents, b.total_cents,
  bb.balance_cents - bb.reserved_cents
  FROM billing.bill b JOIN billing.bill_balance bb ON bb.bill_id = b.id
  WHERE b.id = $1::uuid
  FOR UPDATE OF b`

// WriteOffBill implements WriteOffBills over billing.bill: it locks bill id and reads its
// open balance (net of reserved fee transfers), issued tax and total and its payer. The
// responsible lawyer comes through Lawyers, never a join on matter.matter; a missing one
// fails closed.
func (p PGWriteOffs) WriteOffBill(ctx context.Context, tx pgx.Tx, id string) (WriteOffBill, error) {
	if p.Lawyers == nil {
		return WriteOffBill{}, fmt.Errorf("write-off bill: no responsible-lawyer lookup: %w", ErrInvalidState)
	}
	var (
		b      WriteOffBill
		matter string
	)
	err := tx.QueryRow(ctx, writeOffBillSQL, id).Scan(&b.ID, &matter, &b.ClientID, &b.Delivered, &b.TaxCents, &b.TotalCents, &b.OpenCents)
	if notFound(err) {
		return WriteOffBill{}, fmt.Errorf("write-off bill %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return WriteOffBill{}, fmt.Errorf("write-off bill %s: %w", id, err)
	}
	if b.OpenCents < 0 {
		return WriteOffBill{}, fmt.Errorf("write-off bill %s: negative balance: %w", id, ErrInvalidState)
	}
	lawyer, err := p.Lawyers.ResponsibleLawyer(ctx, tx, matter)
	if err != nil {
		return WriteOffBill{}, fmt.Errorf("write-off bill %s: responsible lawyer: %w", id, err)
	}
	if lawyer == "" {
		return WriteOffBill{}, fmt.Errorf("write-off bill %s: no responsible lawyer: %w", id, ErrInvalidState)
	}
	b.ResponsibleUser = lawyer
	return b, nil
}
