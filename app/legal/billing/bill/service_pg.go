package bill

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TaxCodeExempt is the bill_line tax code of a line outside the tax base (an agent
// disbursement). Every other code is taxable.
const TaxCodeExempt = "EXEMPT"

// SQL the Postgres Service runs. Tests script a pgx.Tx against these exact strings.
const (
	billReadSQL = `
SELECT b.id::text, coalesce(b.number, ''), b.matter_id::text, b.payer_id::text, b.status, coalesce(b.created_by::text, ''),
       b.issue_date, b.due_date, lower(b.period), upper(b.period),
       b.fees_cents, b.taxable_disb_cents, b.nontaxable_disb_cents, b.discount_cents, b.tax_cents, b.total_cents,
       b.interest_rate_ppm, coalesce(b.place_of_supply_province, ''), coalesce(b.approval_id::text, ''),
       coalesce(b.approved_by::text, ''), b.approved_at, coalesce(b.signed_by::text, ''), b.signed_at,
       coalesce(encode(b.signature_hash, 'hex'), ''), b.delivered_at, coalesce(b.delivery_channel, ''),
       b.row_version, b.created_at, b.tax_breakdown,
       b.total_cents - coalesce((SELECT sum(a.amount_cents) FROM billing.payment_allocation a WHERE a.bill_id = b.id), 0)
  FROM billing.bill b WHERE b.id = $1::uuid`
	billLinesSQL = `
SELECT id::text, ordinal, source_type, coalesce(time_entry_id::text, expense_id::text, ''), narrative, quantity_milli,
       rate_cents, amount_cents, tax_code
  FROM billing.bill_line WHERE bill_id = $1::uuid ORDER BY ordinal`
	billLockSQL = `SELECT status FROM billing.bill WHERE id = $1::uuid FOR UPDATE`
	billListSQL = `
SELECT id::text FROM billing.bill
 WHERE ($1 = '' OR matter_id::text = $1)
   AND (cardinality($2::text[]) = 0 OR status = ANY($2::text[]))
   AND ($3 = '' OR id > $3::uuid)
 ORDER BY id
 LIMIT $4`
	billUpdateSQL = `
UPDATE billing.bill SET discount_cents = $2, total_cents = $3, due_date = $4::date, interest_rate_ppm = $5
 WHERE id = $1::uuid AND status IN ('draft', 'preflight_failed', 'pending_approval')`
	billSignSQL = `
UPDATE billing.bill SET signed_by = $2::uuid, signed_at = $3, signature_hash = decode($4, 'hex')
 WHERE id = $1::uuid AND status = 'approved' AND signed_by IS NULL`
	billDeliverSQL = `
UPDATE billing.bill SET status = 'delivered', delivered_at = $2, delivery_channel = $3
 WHERE id = $1::uuid AND status = 'approved' AND delivered_at IS NULL`
	billSubmitSQL = `UPDATE billing.bill SET status = 'pending_approval' WHERE id = $1::uuid AND status = 'draft'`
)

// PGService is the Postgres bill Service over billing.bill and billing.bill_line. Reads run
// in their own transaction on DB; writes run in the caller's tx. Approval goes through the
// same store, number series and checks as the approval gate effect.
type PGService struct {
	DB  TxRunner
	Now func() time.Time // the instant delivery evidence is checked against
	// Next issues the bill number; nil means NextNumber.
	Next func(ctx context.Context, tx pgx.Tx, prefix string, year int) (string, error)
}

var _ Service = (*PGService)(nil)

// NewPGService returns the Postgres Service on db.
func NewPGService(db TxRunner, now func() time.Time) *PGService {
	return &PGService{DB: db, Now: now}
}

// notFound maps a missing row or a malformed id to ErrNotFound.
func notFound(op string, id ID, err error) error {
	var pg *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
		return fmt.Errorf("%s %s: %w", op, id, ErrNotFound)
	}
	return fmt.Errorf("%s %s: %w", op, id, err)
}

// read loads bill id and its lines in tx.
func (s *PGService) read(ctx context.Context, tx pgx.Tx, id ID) (Bill, error) {
	var (
		b                                          Bill
		issue, due, from, to, approved, signed, dl *time.Time
		rate                                       *int64
		channel                                    string
		taxes                                      []byte
	)
	err := tx.QueryRow(ctx, billReadSQL, string(id)).Scan(
		&b.ID, &b.Number, &b.MatterID, &b.PayerID, &b.Status, &b.PreparedBy,
		&issue, &due, &from, &to,
		&b.FeesCents, &b.TaxableDisbCents, &b.NontaxableDisbCents, &b.DiscountCents, &b.TaxCents, &b.TotalCents,
		&rate, &b.PlaceOfSupplyProvince, &b.ApprovalID,
		&b.ApprovedBy, &approved, &b.SignedBy, &signed,
		&b.SignatureHash, &dl, &channel,
		&b.Version, &b.CreatedAt, &taxes, &b.BalanceCents)
	if err != nil {
		return Bill{}, notFound("read bill", id, err)
	}
	b.IssueDate, b.DueDate, b.PeriodFrom = dateOf(issue), dateOf(due), dateOf(from)
	if to != nil {
		// The period is a [from, to) daterange; the bill shows its last day.
		b.PeriodTo = dateOf(to).AddDate(0, 0, -1)
	}
	b.InterestRatePPM = rate
	if approved != nil {
		b.ApprovedAt = *approved
	}
	if signed != nil {
		b.SignedAt = *signed
	}
	if b.SignatureHash != "" {
		b.SignatureHash = "sha256:" + b.SignatureHash
	}
	if dl != nil {
		b.DeliveredAt = *dl
		b.DeliveredOn = businessDate(*dl)
	}
	b.DeliveryChannel = Channel(channel)
	var tl []TaxLine
	if len(taxes) > 0 && json.Unmarshal(taxes, &tl) == nil && len(tl) > 0 {
		b.TaxRatePPM = tl[0].RatePPM
	}
	if b.Lines, err = s.lines(ctx, tx, id); err != nil {
		return Bill{}, err
	}
	return b, nil
}

// dateOf is a stored date at midnight America/Toronto; nil is the zero time.
func dateOf(d *time.Time) time.Time {
	if d == nil {
		return time.Time{}
	}
	y, m, day := d.Date()
	return time.Date(y, m, day, 0, 0, 0, 0, business)
}

func (s *PGService) lines(ctx context.Context, tx pgx.Tx, id ID) ([]Line, error) {
	rows, err := tx.Query(ctx, billLinesSQL, string(id))
	if err != nil {
		return nil, fmt.Errorf("lines of bill %s: %w", id, err)
	}
	defer rows.Close()
	out := []Line{}
	for rows.Next() {
		var l Line
		var src string
		if err := rows.Scan(&l.ID, &l.Ordinal, &src, &l.SourceID, &l.Narrative, &l.QuantityMilli,
			&l.RateCents, &l.AmountCents, &l.TaxCode); err != nil {
			return nil, fmt.Errorf("lines of bill %s: %w", id, err)
		}
		l.Source = SourceType(src)
		l.Taxable = l.TaxCode != TaxCodeExempt
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lines of bill %s: %w", id, err)
	}
	return out, nil
}

// lock takes the bill row lock in tx and returns the bill.
func (s *PGService) lock(ctx context.Context, tx pgx.Tx, op string, id ID) (Bill, error) {
	var st string
	if err := tx.QueryRow(ctx, billLockSQL, string(id)).Scan(&st); err != nil {
		return Bill{}, notFound(op, id, err)
	}
	return s.read(ctx, tx, id)
}

// Get returns one bill.
func (s *PGService) Get(ctx context.Context, id ID) (Bill, error) {
	var b Bill
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		b, err = s.read(ctx, tx, id)
		return err
	})
	return b, err
}

// List returns bills by id after f.After, at most f.Limit (0: all), filtered by matter and
// status.
func (s *PGService) List(ctx context.Context, f Filter) ([]Bill, error) {
	statuses := make([]string, 0, len(f.Statuses))
	for _, st := range f.Statuses {
		statuses = append(statuses, string(st))
	}
	var limit *int
	if f.Limit > 0 {
		limit = &f.Limit
	}
	out := []Bill{}
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, billListSQL, f.MatterID, statuses, string(f.After), limit)
		if err != nil {
			return fmt.Errorf("list bills: %w", err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("list bills: %w", err)
		}
		for _, id := range ids {
			b, err := s.read(ctx, tx, ID(id))
			if err != nil {
				return err
			}
			out = append(out, b)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Update edits a draft's discount, due date and interest rate. Lines change only through
// billing/draft, which recomputes the totals; a line change here is ErrInvalid.
func (s *PGService) Update(ctx context.Context, tx pgx.Tx, id ID, c Change) (Bill, error) {
	b, err := s.lock(ctx, tx, "update", id)
	if err != nil {
		return Bill{}, err
	}
	// rule: BIL-C-024
	next, err := applyChange(b, c)
	if err != nil {
		return Bill{}, err
	}
	if c.Lines != nil {
		return Bill{}, fmt.Errorf("update %s: lines change through the draft: %w", id, ErrInvalid)
	}
	var due *string
	if !next.DueDate.IsZero() {
		d := next.DueDate.In(business).Format(time.DateOnly)
		due = &d
	}
	tag, err := tx.Exec(ctx, billUpdateSQL, string(id), next.DiscountCents, next.TotalCents, due, next.InterestRatePPM)
	if err != nil {
		return Bill{}, fmt.Errorf("update %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return Bill{}, fmt.Errorf("update %s: %w", id, ErrBillGuard)
	}
	return s.read(ctx, tx, id)
}

// Sign records the licensee's signature on an approved, unsigned bill.
func (s *PGService) Sign(ctx context.Context, tx pgx.Tx, id ID, sig Signature) error {
	hexHash, ok := strings.CutPrefix(sig.Hash, "sha256:")
	if _, err := hex.DecodeString(hexHash); !ok || err != nil || hexHash == "" || sig.SignedBy == "" || sig.SignedAt.IsZero() {
		return fmt.Errorf("sign %s: %w", id, ErrInvalid)
	}
	b, err := s.lock(ctx, tx, "sign", id)
	if err != nil {
		return err
	}
	switch {
	case b.Status != StatusApproved:
		return fmt.Errorf("sign %s: %w", id, ErrNotApproved)
	case b.SignedBy != "":
		return fmt.Errorf("sign %s: signature: %w", id, ErrBillGuard)
	}
	tag, err := tx.Exec(ctx, billSignSQL, string(id), sig.SignedBy, sig.SignedAt, hexHash)
	if err != nil {
		return fmt.Errorf("sign %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("sign %s: %w", id, ErrBillGuard)
	}
	return nil
}

// Deliver records delivery evidence: it needs a signature and sets DeliveredAt once. A
// bounce leaves the bill undelivered.
func (s *PGService) Deliver(ctx context.Context, tx pgx.Tx, id ID, ev DeliveryEvidence) (Bill, error) {
	b, err := s.lock(ctx, tx, "deliver", id)
	if err != nil {
		return Bill{}, err
	}
	// rule: BIL-L-003
	// Delivery is evidence from a channel, after approval and signature, never in the future.
	switch {
	case !b.DeliveredAt.IsZero():
		return Bill{}, fmt.Errorf("deliver %s: %w", id, ErrAlreadyDelivered)
	case b.Status != StatusApproved:
		return Bill{}, fmt.Errorf("deliver %s: %w", id, ErrNotApproved)
	case b.SignedBy == "":
		return Bill{}, fmt.Errorf("deliver %s: %w", id, ErrSignatureRequired)
	case ev.AcceptedAt.IsZero():
		return Bill{}, fmt.Errorf("deliver %s: %w", id, ErrInvalid)
	case s.Now == nil || ev.AcceptedAt.After(s.Now()):
		return Bill{}, fmt.Errorf("deliver %s: %w", id, ErrFutureEvidence)
	}
	if ev.Bounced {
		return b, nil
	}
	tag, err := tx.Exec(ctx, billDeliverSQL, string(id), ev.AcceptedAt, string(ev.Channel))
	if err != nil {
		return Bill{}, fmt.Errorf("deliver %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return Bill{}, fmt.Errorf("deliver %s: %w", id, ErrAlreadyDelivered)
	}
	return s.read(ctx, tx, id)
}

// Submit moves a draft to pending_approval in tx, the tx that opens the approval gate.
func (s *PGService) Submit(ctx context.Context, tx pgx.Tx, id ID) (Bill, error) {
	b, err := s.lock(ctx, tx, "submit", id)
	if err != nil {
		return Bill{}, err
	}
	if b.Status != StatusDraft {
		return Bill{}, fmt.Errorf("submit %s in %s: %w", id, b.Status, ErrNotSubmittable)
	}
	// rule: BIL-C-120
	// Submit only moves the draft to pending; approval is the gate effect's alone.
	tag, err := tx.Exec(ctx, billSubmitSQL, string(id))
	if err != nil {
		return Bill{}, fmt.Errorf("submit %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return Bill{}, fmt.Errorf("submit %s: %w", id, ErrNotSubmittable)
	}
	return s.read(ctx, tx, id)
}
