package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/money"
)

// ErrBillNotOpen is returned when a payment is applied to a bill that is not delivered or
// partially paid. It is also ErrInvalid.
var ErrBillNotOpen = fmt.Errorf("%w: bill is not open for payment", ErrInvalid)

// ErrNoPayerNames is returned by Capture while PGService has no PayerNames: a payment is never
// written without its payer's name.
var ErrNoPayerNames = errors.New("payment: payer name reader not wired")

// PayerNames is the party reader port billing needs to name a payer. Billing never reads party
// tables (ARC-I-041); boot wires this to the party module.
type PayerNames interface {
	// PayerName returns the legal name of the client contact clientID.
	PayerName(ctx context.Context, clientID string) (string, error)
}

// PGService is Service over billing.payment and billing.payment_allocation (0001_init.sql),
// with billing.payment_capture_key and billing.payment_reversal (5630). It serves operating
// payments only: a trust deposit is a trust receipt and is never read or written here.
type PGService struct {
	DB TxRunner
	// Cash is the By-Law 9 cash limit; while nil a cash payment fails closed.
	Cash CashLimit
	// Payers names the payer of a capture; while nil a capture fails closed.
	Payers PayerNames
	// GL returns the operating ledger posting as the given user; the receipt, each applied
	// bill row and every reversal are journaled in the same tx. While nil a capture or a
	// reversal fails closed.
	GL func(postedBy string) ledger.Ledger
	// Clock dates reversal entries in the business time zone; while nil a reversal fails closed.
	Clock clock.Clock
}

var _ Service = PGService{}

// operating restricts every read to operating payments.
// rule: BIL-L-034
// Trust money goes through trust receipts, never through the operating payment service.
const operating = `p.deposit_to = 'operating'`

// paymentCols reads one payment row; the bill is the first bill the payment was applied to.
const paymentCols = `p.id::text, p.received_on, coalesce(p.payer_contact_id::text, ''), p.amount_cents, p.method,
  coalesce(p.provider, ''), coalesce(p.provider_ref, ''), coalesce(p.created_by::text, ''), p.status,
  coalesce(r.reason, ''), coalesce(k.idempotency_key, ''), coalesce(r.reversed_by::text, ''),
  coalesce((SELECT a.bill_id::text FROM billing.payment_allocation a
    WHERE a.payment_id = p.id AND a.bill_id IS NOT NULL AND a.reverses_id IS NULL ORDER BY a.at, a.id LIMIT 1), '')`

const paymentFrom = ` FROM billing.payment p
  LEFT JOIN billing.payment_reversal r ON r.payment_id = p.id
  LEFT JOIN billing.payment_capture_key k ON k.payment_id = p.id`

const getPaymentSQL = `SELECT ` + paymentCols + paymentFrom + ` WHERE p.id = $1::uuid AND ` + operating

const listPaymentsSQL = `SELECT ` + paymentCols + paymentFrom + ` WHERE ` + operating + `
  AND ($1 = '' OR p.id > $1::uuid)
  AND ($2 = '' OR p.payer_contact_id = $2::uuid)
  AND ($3::date IS NULL OR p.received_on >= $3::date)
  AND ($4::date IS NULL OR p.received_on <= $4::date)
ORDER BY p.id LIMIT $5`

// allocationsSQL lists a payment's applied allocations in the order they were made;
// reversal rows are not allocations of their own.
const allocationsSQL = `SELECT coalesce(a.bill_id::text, ''), a.fee_cents + a.dtx_cents + a.dnt_cents + a.interest_cents, a.tax_cents
FROM billing.payment_allocation a WHERE a.payment_id = $1::uuid AND a.reverses_id IS NULL ORDER BY a.at, a.id`

const byProviderSQL = `SELECT p.id::text FROM billing.payment p WHERE p.provider = $1 AND p.provider_ref = $2 AND ` + operating

const byKeySQL = `SELECT k.payment_id::text FROM billing.payment_capture_key k WHERE k.idempotency_key = $1`

// lockBillSQL locks a bill and reads what is open on it: the balance less reserved fee
// transfers, and the tax not yet paid. exclude is a payment whose own allocations count as open.
const lockBillSQL = `SELECT b.payer_id::text, b.status,
  (bb.balance_cents - bb.reserved_cents + coalesce((SELECT sum(a.amount_cents) FROM billing.payment_allocation a
    WHERE a.bill_id = b.id AND a.payment_id = $2::uuid), 0))::bigint,
  (b.tax_cents - coalesce((SELECT sum(a.tax_cents) FROM billing.payment_allocation a
    WHERE a.bill_id = b.id AND a.payment_id IS DISTINCT FROM $2::uuid), 0))::bigint
FROM billing.bill b JOIN billing.bill_balance bb ON bb.bill_id = b.id
WHERE b.id = $1::uuid FOR UPDATE OF b`

const insertPaymentSQL = `INSERT INTO billing.payment (received_on, payer_contact_id, payer_name, amount_cents, method,
  deposit_to, provider, provider_ref, status, created_by)
VALUES ($1::date, $2::uuid, $9, $3, $4, 'operating', NULLIF($5, ''), NULLIF($6, ''), $7, $8::uuid) RETURNING id::text`

const insertAllocationSQL = `INSERT INTO billing.payment_allocation (payment_id, target_type, bill_id, amount_cents,
  fee_cents, tax_cents, allocated_by) VALUES ($1::uuid, 'bill', $2::uuid, $3, $4, $5, $6::uuid) RETURNING id::text`

const insertKeySQL = `INSERT INTO billing.payment_capture_key (idempotency_key, payment_id) VALUES ($1, $2::uuid)`

const lockPaymentSQL = `SELECT p.status, p.amount_cents FROM billing.payment p WHERE p.id = $1::uuid AND ` + operating + ` FOR UPDATE OF p`

// reverseAllocationsSQL writes -1 x every applied allocation, bucket for bucket (BIL-I-019),
// allocated by the user who reverses, never the original allocator.
const reverseAllocationsSQL = `INSERT INTO billing.payment_allocation (payment_id, target_type, bill_id, trust_ledger_id,
  client_id, amount_cents, fee_cents, dtx_cents, dnt_cents, tax_cents, interest_cents, reverses_id, allocated_by)
SELECT a.payment_id, a.target_type, a.bill_id, a.trust_ledger_id, a.client_id, -a.amount_cents, -a.fee_cents,
  -a.dtx_cents, -a.dnt_cents, -a.tax_cents, -a.interest_cents, a.id, $2::uuid
FROM billing.payment_allocation a WHERE a.payment_id = $1::uuid AND a.reverses_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM billing.payment_allocation x WHERE x.reverses_id = a.id)
ORDER BY a.at, a.id
RETURNING id::text, target_type, amount_cents`

const insertReversalSQL = `INSERT INTO billing.payment_reversal (payment_id, reason, reversed_by) VALUES ($1::uuid, $2, $3::uuid)`

// humanSQL says whether a user is a human principal; no row means no such user.
const humanSQL = `SELECT u.principal_kind = 'human' FROM core.app_user u WHERE u.id = $1::uuid`

const markReversedSQL = `UPDATE billing.payment SET status = 'reversed' WHERE id = $1::uuid`

// statusReversed is billing.payment.status of a reversed payment.
const statusReversed = "reversed"

// pgMethods are the billing.payment.method values.
var pgMethods = map[string]bool{"cheque": true, "eft": true, "wire": true, "cash": true, "card": true, "bank_draft": true, "interac": true}

// Capture implements Service. The bill is locked, the cash limit is checked and the payment
// and its allocation are written in one transaction.
func (s PGService) Capture(ctx context.Context, c Capture) (Payment, []Allocation, error) {
	if c.Kind == KindRetainer {
		// rule: BIL-L-034
		// Retainer money is trust money: it is a trust receipt, never an operating payment.
		return Payment{}, nil, ErrRetainerToOperating
	}
	// rule: BIL-C-064
	// Integer cents only; a payment is never zero or negative.
	if c.AmountCents <= 0 || c.ClientID == "" || c.BillID == "" || c.RecordedBy == "" || !pgMethods[c.Method] ||
		(c.Provider == "") != (c.ProviderEventID == "") {
		return Payment{}, nil, fmt.Errorf("capture: %w", ErrInvalid)
	}
	if s.Payers == nil {
		return Payment{}, nil, ErrNoPayerNames
	}
	if s.GL == nil {
		return Payment{}, nil, ErrNoGL
	}
	name, err := s.Payers.PayerName(ctx, c.ClientID)
	if err != nil {
		return Payment{}, nil, fmt.Errorf("capture: payer name: %w", err)
	}
	if name == "" {
		return Payment{}, nil, fmt.Errorf("capture: payer has no name: %w", ErrInvalid)
	}
	var p Payment
	var as []Allocation
	err = s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if id, err := s.prior(ctx, tx, c); err != nil || id != "" {
			if err != nil {
				return err
			}
			p, as, err = s.read(ctx, tx, id)
			return err
		}
		var err error
		p, as, err = s.capture(ctx, tx, c, name)
		return err
	})
	if err != nil {
		return Payment{}, nil, err
	}
	return p, as, nil
}

// prior returns the payment an earlier capture with the same provider event or idempotency key
// recorded, or "".
func (PGService) prior(ctx context.Context, tx pgx.Tx, c Capture) (string, error) {
	sql, args := byKeySQL, []any{c.IdempotencyKey}
	switch {
	case c.Provider != "":
		sql, args = byProviderSQL, []any{c.Provider, c.ProviderEventID}
	case c.IdempotencyKey == "":
		return "", nil
	}
	var id string
	err := tx.QueryRow(ctx, sql, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("capture dedup: %w", err)
	}
	return id, nil
}

// openBill is what is still owed on a locked bill.
type openBill struct {
	payer, status string
	total, tax    int64
}

func lockBill(ctx context.Context, tx pgx.Tx, billID, paymentID string) (openBill, error) {
	var b openBill
	var exclude any
	if paymentID != "" {
		exclude = paymentID
	}
	err := tx.QueryRow(ctx, lockBillSQL, billID, exclude).Scan(&b.payer, &b.status, &b.total, &b.tax)
	if missing(err) {
		return openBill{}, fmt.Errorf("bill %s: %w", billID, ErrNotFound)
	}
	if err != nil {
		return openBill{}, fmt.Errorf("lock bill %s: %w", billID, err)
	}
	b.total = max(b.total, 0)
	b.tax = min(max(b.tax, 0), b.total)
	return b, nil
}

// payable checks a locked bill can take a payment from client.
func (b openBill) payable(client string) error {
	if b.payer != client {
		return ErrOtherClient
	}
	if b.status != "delivered" && b.status != "partially_paid" {
		return ErrBillNotOpen
	}
	return nil
}

// split is the tax share rhu(applied*open_tax, open_total) and the rest as fees (BIL-C-002).
func (b openBill) split(applied int64) (fee, tax int64, err error) {
	if applied == 0 || b.total == 0 {
		return applied, 0, nil
	}
	tax, err = money.MulRhu(applied, b.tax, b.total)
	if err != nil {
		return 0, 0, fmt.Errorf("allocate tax: %w", err)
	}
	return applied - tax, tax, nil
}

func (s PGService) capture(ctx context.Context, tx pgx.Tx, c Capture, payer string) (Payment, []Allocation, error) {
	b, err := lockBill(ctx, tx, c.BillID, "")
	if err != nil {
		return Payment{}, nil, err
	}
	if err := b.payable(c.ClientID); err != nil {
		return Payment{}, nil, err
	}
	if err := CheckCash(ctx, tx, s.Cash, c); err != nil {
		return Payment{}, nil, err
	}
	// rule: DM-045
	// The allocation never exceeds the payment or what is open on the bill; the rest is unapplied.
	applied := min(c.AmountCents, b.total)
	status := "posted"
	if applied < c.AmountCents {
		status = "unallocated"
	}
	var id string
	if err := tx.QueryRow(ctx, insertPaymentSQL, c.ReceivedOn.Format(time.DateOnly), c.ClientID, c.AmountCents, c.Method,
		c.Provider, c.ProviderEventID, status, c.RecordedBy, payer).Scan(&id); err != nil {
		return Payment{}, nil, fmt.Errorf("capture: %w", err)
	}
	gl := s.GL(c.RecordedBy)
	on := clock.DateOf(c.ReceivedOn, c.ReceivedOn.Location())
	// rule: BIL-L-042
	// At receipt the whole payment is Dr operating bank / Cr unapplied; trust money never
	// reaches this service (BIL-L-034).
	if _, err := gl.Post(ctx, tx, receiptEntry(id, c.AmountCents, on)); err != nil {
		return Payment{}, nil, fmt.Errorf("capture %s: post receipt: %w", id, err)
	}
	var as []Allocation
	if applied > 0 {
		fee, tax, err := b.split(applied)
		if err != nil {
			return Payment{}, nil, err
		}
		var allocID string
		if err := tx.QueryRow(ctx, insertAllocationSQL, id, c.BillID, applied, fee, tax, c.RecordedBy).Scan(&allocID); err != nil {
			return Payment{}, nil, fmt.Errorf("capture %s: allocate: %w", id, err)
		}
		if _, err := gl.Post(ctx, tx, applyEntry(allocID, id, applied, on)); err != nil {
			return Payment{}, nil, fmt.Errorf("capture %s: post allocation: %w", id, err)
		}
		as = append(as, Allocation{PaymentID: id, BillID: c.BillID, FeeCents: fee, TaxCents: tax})
	}
	if rest := c.AmountCents - applied; rest > 0 {
		as = append(as, Allocation{PaymentID: id, FeeCents: rest})
	}
	if c.Provider == "" && c.IdempotencyKey != "" {
		if _, err := tx.Exec(ctx, insertKeySQL, c.IdempotencyKey, id); err != nil {
			return Payment{}, nil, fmt.Errorf("capture %s: idempotency key: %w", id, err)
		}
	}
	return Payment{ID: id, Capture: c, Status: StatusCaptured}, as, nil
}

// read returns a payment and its allocations in tx.
func (s PGService) read(ctx context.Context, tx pgx.Tx, id string) (Payment, []Allocation, error) {
	p, err := scanPayment(tx.QueryRow(ctx, getPaymentSQL, id))
	if missing(err) {
		return Payment{}, nil, fmt.Errorf("payment %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Payment{}, nil, fmt.Errorf("payment %s: %w", id, err)
	}
	as, err := allocationsOf(ctx, tx, p)
	return p, as, err
}

// allocationsOf lists p's applied allocations, then any unapplied remainder as one row with
// no bill.
func allocationsOf(ctx context.Context, tx pgx.Tx, p Payment) ([]Allocation, error) {
	rows, err := tx.Query(ctx, allocationsSQL, p.ID)
	if err != nil {
		return nil, fmt.Errorf("allocations of %s: %w", p.ID, err)
	}
	as, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Allocation, error) {
		a := Allocation{PaymentID: p.ID}
		return a, r.Scan(&a.BillID, &a.FeeCents, &a.TaxCents)
	})
	if err != nil {
		return nil, fmt.Errorf("allocations of %s: %w", p.ID, err)
	}
	var sum int64
	for _, a := range as {
		sum += a.Total()
	}
	if rest := p.Capture.AmountCents - sum; rest > 0 {
		as = append(as, Allocation{PaymentID: p.ID, FeeCents: rest})
	}
	return as, nil
}

// businessDate maps a scanned date to midnight America/Toronto.
func businessDate(d time.Time) time.Time {
	loc, err := businessZone()
	if err != nil {
		return d
	}
	y, m, day := d.Date()
	return time.Date(y, m, day, 0, 0, 0, 0, loc)
}

func scanPayment(row pgx.Row) (Payment, error) {
	var p Payment
	var on time.Time
	var status, reason, by string
	c := &p.Capture
	if err := row.Scan(&p.ID, &on, &c.ClientID, &c.AmountCents, &c.Method, &c.Provider, &c.ProviderEventID,
		&c.RecordedBy, &status, &reason, &c.IdempotencyKey, &by, &c.BillID); err != nil {
		return Payment{}, err
	}
	c.Kind, c.ReceivedOn = KindBillPayment, businessDate(on)
	p.Status = StatusCaptured
	if status == statusReversed {
		p.Status, p.Reason, p.ReversedBy = StatusReversed, ReversalReason(reason), by
	}
	return p, nil
}

// Reverse implements Service: every applied allocation gets its -1 x row, bucket for bucket,
// and the payment is marked reversed with its reason and the user who reversed it.
func (s PGService) Reverse(ctx context.Context, id string, reason ReversalReason, by Actor) (Payment, error) {
	if reason != ReasonNSF && reason != ReasonRefund && reason != ReasonChargeback {
		return Payment{}, fmt.Errorf("reverse %s: reason %q: %w", id, reason, ErrInvalid)
	}
	// The agent never reverses money; the books record the human who did.
	if by.Agent {
		return Payment{}, ErrAgentReverse
	}
	if by.UserID == "" {
		return Payment{}, fmt.Errorf("reverse %s: no actor: %w", id, ErrInvalid)
	}
	if s.GL == nil || s.Clock == nil {
		return Payment{}, ErrNoGL
	}
	var p Payment
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var human bool
		err := tx.QueryRow(ctx, humanSQL, by.UserID).Scan(&human)
		if missing(err) {
			return fmt.Errorf("reverse %s: unknown user: %w", id, ErrInvalid)
		}
		if err != nil {
			return fmt.Errorf("reverse %s: actor: %w", id, err)
		}
		if !human {
			return ErrAgentReverse
		}
		var status string
		var amount int64
		err = tx.QueryRow(ctx, lockPaymentSQL, id).Scan(&status, &amount)
		if missing(err) {
			return fmt.Errorf("reverse %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("reverse %s: %w", id, err)
		}
		if status == statusReversed {
			p, _, err = s.read(ctx, tx, id)
			if err != nil {
				return err
			}
			return ErrReversed
		}
		if err := s.reverseBooks(ctx, tx, id, amount, by.UserID); err != nil {
			return err
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{insertReversalSQL, []any{id, string(reason), by.UserID}},
			{markReversedSQL, []any{id}},
		} {
			if _, err := tx.Exec(ctx, q.sql, q.args...); err != nil {
				return fmt.Errorf("reverse %s: %w", id, err)
			}
		}
		p, _, err = s.read(ctx, tx, id)
		return err
	})
	if errors.Is(err, ErrReversed) {
		return p, ErrReversed
	}
	if err != nil {
		return Payment{}, err
	}
	return p, nil
}

// reverseRow is one -1 x allocation row reverseAllocationsSQL wrote.
type reverseRow struct {
	id, target string
	cents      int64
}

// reverseBooks writes the -1 x allocation rows and posts the mirror entries: Dr AR /
// Cr unapplied per reversed bill row, then Dr unapplied / Cr operating bank for the receipt.
func (s PGService) reverseBooks(ctx context.Context, tx pgx.Tx, id string, amount int64, by string) error {
	// rule: BIL-I-019
	// A reversal mirrors each original allocation in every bucket.
	rows, err := tx.Query(ctx, reverseAllocationsSQL, id, by)
	if err != nil {
		return fmt.Errorf("reverse %s: %w", id, err)
	}
	rs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (reverseRow, error) {
		var x reverseRow
		return x, r.Scan(&x.id, &x.target, &x.cents)
	})
	if err != nil {
		return fmt.Errorf("reverse %s: %w", id, err)
	}
	gl, today := s.GL(by), s.Clock.Today(ctx)
	for _, r := range rs {
		if r.target != "bill" || r.cents == 0 {
			continue
		}
		if _, err := gl.Post(ctx, tx, applyEntry(r.id, id, r.cents, today)); err != nil {
			return fmt.Errorf("reverse %s: post allocation: %w", id, err)
		}
	}
	if _, err := gl.Post(ctx, tx, receiptReversalEntry(id, amount, today)); err != nil {
		return fmt.Errorf("reverse %s: post receipt: %w", id, err)
	}
	return nil
}

// Get implements Service.
func (s PGService) Get(ctx context.Context, id string) (Payment, error) {
	var p Payment
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		p, err = scanPayment(tx.QueryRow(ctx, getPaymentSQL, id))
		if missing(err) {
			return fmt.Errorf("payment %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("payment %s: %w", id, err)
		}
		return nil
	})
	return p, err
}

// Allocations implements Service.
func (s PGService) Allocations(ctx context.Context, id string) ([]Allocation, error) {
	var as []Allocation
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		_, as, err = s.read(ctx, tx, id)
		return err
	})
	return as, err
}

// List implements Service.
func (s PGService) List(ctx context.Context, f ListFilter) ([]Payment, error) {
	if f.Limit <= 0 {
		return nil, nil
	}
	date := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return t.Format(time.DateOnly)
	}
	var out []Payment
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, listPaymentsSQL, f.After, f.ClientID, date(f.ReceivedFrom), date(f.ReceivedTo), f.Limit)
		if err != nil {
			return fmt.Errorf("list payments: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Payment, error) { return scanPayment(r) })
		if err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "22P02" {
				return nil
			}
			return fmt.Errorf("list payments: %w", err)
		}
		return nil
	})
	return out, err
}
