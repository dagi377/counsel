package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates/decider"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/registry"
)

// AllocationGLSource is the gl.journal_entry source_type of an applied allocation; the source
// id is the billing.payment_allocation id, so each row posts once.
const AllocationGLSource = "payment_allocation"

// ErrTrustMoney is returned when an allocation would move trust money: a payment deposited to
// trust, or an operating payment applied to a trust ledger (By-law 9).
var ErrTrustMoney = errors.New("payment: trust money is not allocated here")

// AllocPayment is a payment as the allocation effect reads it under lock.
type AllocPayment struct {
	ID             string
	ClientID       string
	AmountCents    int64
	AllocatedCents int64 // net of reversals
	DepositTo      string
	Status         string
}

// AllocBill is what is open on a bill, locked in the gate tx.
type AllocBill struct {
	PayerID   string
	Status    string
	OpenCents int64
	OpenTax   int64
}

// AppliedAllocation is one billing.payment_allocation row the effect writes.
type AppliedAllocation struct {
	PaymentID  string
	BillID     string // "" for unapplied money (client credit)
	ClientID   string // unapplied money only
	FeeCents   int64
	TaxCents   int64
	ApprovalID string
	By         string
}

// Total is the row's amount.
func (a AppliedAllocation) Total() int64 { return a.FeeCents + a.TaxCents }

// AllocationStore applies allocations. Every method runs in the gate tx. PGAllocations
// implements it.
type AllocationStore interface {
	// LockPayment locks payment id and returns it, or ErrNotFound.
	LockPayment(ctx context.Context, tx pgx.Tx, id string) (AllocPayment, error)
	// AppliedUnder reports whether rows were already applied under gate id.
	AppliedUnder(ctx context.Context, tx pgx.Tx, gateID string) (bool, error)
	// LockBill locks bill id and returns what is open on it, or ErrNotFound.
	LockBill(ctx context.Context, tx pgx.Tx, id string) (AllocBill, error)
	// Insert writes one allocation row and returns its id.
	Insert(ctx context.Context, tx pgx.Tx, a AppliedAllocation) (string, error)
	// Settle moves a bill to partially_paid or paid from its derived balance.
	Settle(ctx context.Context, tx pgx.Tx, billID string) error
}

// allocationPayload is the billing.payment_allocation gate payload (the HTTP-15 route).
type allocationPayload struct {
	PaymentID   string `json:"payment_id"`
	Allocations []struct {
		TargetType  string `json:"target_type"` // bill, trust_ledger or unapplied
		TargetID    string `json:"target_id"`
		AmountCents int64  `json:"amount_cents"`
	} `json:"allocations"`
}

// AllocationEffect is the billing.payment_allocation EffectFunc: in the gate tx it writes the
// approved allocation rows on the operating payment, moves each bill's status and posts
// Dr unapplied / Cr AR fees per bill. The receipt already posted Dr operating bank /
// Cr unapplied when the payment was captured, so money left unapplied posts nothing.
type AllocationEffect struct {
	Store  AllocationStore
	Ledger ledger.Ledger
	Clock  clock.Clock
}

// Apply applies the approved allocation set. A gate already applied is a no-op.
func (e *AllocationEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	by, err := decider.Check(g)
	if err != nil {
		return fmt.Errorf("allocation effect: %w", err)
	}
	if e == nil || e.Store == nil || e.Ledger == nil || e.Clock == nil {
		return fmt.Errorf("allocation effect %s: not wired: %w", g.GateID, ErrInvalid)
	}
	var p allocationPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.PaymentID == "" || len(p.Allocations) == 0 ||
		(g.Subject.ID != "" && g.Subject.ID != p.PaymentID) {
		return fmt.Errorf("allocation effect %s: payload: %w", g.GateID, ErrInvalid)
	}
	pay, err := e.Store.LockPayment(ctx, tx, p.PaymentID)
	if err != nil {
		return fmt.Errorf("allocation effect %s: %w", g.GateID, err)
	}
	// rule: ARC-I-012
	// Idempotent per gate: rows this gate applied are not applied again.
	done, err := e.Store.AppliedUnder(ctx, tx, g.GateID)
	if err != nil {
		return fmt.Errorf("allocation effect %s: %w", g.GateID, err)
	}
	if done {
		return nil
	}
	// rule: BIL-L-034
	// Trust money never moves through an operating allocation (By-law 9).
	if pay.DepositTo != DepositOperating {
		return fmt.Errorf("allocation effect %s: %w", g.GateID, ErrTrustMoney)
	}
	if pay.Status == statusReversed {
		return fmt.Errorf("allocation effect %s: %w", g.GateID, ErrReversed)
	}
	rows, err := e.rows(ctx, tx, g, p, pay, by)
	if err != nil {
		return err
	}
	today := e.Clock.Today(ctx)
	for _, a := range rows {
		id, err := e.Store.Insert(ctx, tx, a)
		if err != nil {
			return fmt.Errorf("allocation effect %s: %w", g.GateID, err)
		}
		if a.BillID == "" {
			// Money left unapplied stays in the unapplied account the receipt credited.
			continue
		}
		if err := e.Store.Settle(ctx, tx, a.BillID); err != nil {
			return fmt.Errorf("allocation effect %s: %w", g.GateID, err)
		}
		// rule: BIL-L-042
		// One balanced entry per applied bill row, in integer cents: Dr unapplied / Cr AR.
		if _, err := e.Ledger.Post(ctx, tx, applyEntry(id, a.PaymentID, a.Total(), today)); err != nil {
			return fmt.Errorf("allocation effect %s: post: %w", g.GateID, err)
		}
	}
	return nil
}

// rows checks the approved set against the payment and the bills and splits each bill row.
func (e *AllocationEffect) rows(ctx context.Context, tx pgx.Tx, g registry.Approved, p allocationPayload, pay AllocPayment, by string) ([]AppliedAllocation, error) {
	var sum int64
	seen := map[string]bool{}
	out := make([]AppliedAllocation, 0, len(p.Allocations))
	for _, r := range p.Allocations {
		if r.AmountCents <= 0 {
			return nil, fmt.Errorf("allocation effect %s: amount %d: %w", g.GateID, r.AmountCents, ErrInvalid)
		}
		sum += r.AmountCents
		switch r.TargetType {
		case "trust_ledger":
			// rule: BIL-L-034
			return nil, fmt.Errorf("allocation effect %s: trust ledger %s: %w", g.GateID, r.TargetID, ErrTrustMoney)
		case "unapplied":
			out = append(out, AppliedAllocation{PaymentID: pay.ID, ClientID: pay.ClientID, FeeCents: r.AmountCents,
				ApprovalID: g.GateID, By: by})
		case "bill":
			if r.TargetID == "" || seen[r.TargetID] {
				return nil, fmt.Errorf("allocation effect %s: bill %q: %w", g.GateID, r.TargetID, ErrInvalid)
			}
			seen[r.TargetID] = true
			b, err := e.Store.LockBill(ctx, tx, r.TargetID)
			if err != nil {
				return nil, fmt.Errorf("allocation effect %s: %w", g.GateID, err)
			}
			ob := openBill{payer: b.PayerID, status: b.Status, total: max(b.OpenCents, 0), tax: min(max(b.OpenTax, 0), max(b.OpenCents, 0))}
			if err := ob.payable(pay.ClientID); err != nil {
				return nil, fmt.Errorf("allocation effect %s: bill %s: %w", g.GateID, r.TargetID, err)
			}
			// rule: DM-047
			// A row never pays more than is open on its bill.
			if r.AmountCents > ob.total {
				return nil, fmt.Errorf("allocation effect %s: bill %s has %d cents open: %w", g.GateID, r.TargetID, ob.total, ErrInvalid)
			}
			// rule: ARC-I-029
			// The bucket split is computed here, never taken from the payload.
			fee, tax, err := ob.split(r.AmountCents)
			if err != nil {
				return nil, fmt.Errorf("allocation effect %s: %w", g.GateID, err)
			}
			out = append(out, AppliedAllocation{PaymentID: pay.ID, BillID: r.TargetID, FeeCents: fee, TaxCents: tax,
				ApprovalID: g.GateID, By: by})
		default:
			return nil, fmt.Errorf("allocation effect %s: target %q: %w", g.GateID, r.TargetType, ErrInvalid)
		}
	}
	// rule: DM-045
	// Never allocate more than is left of the payment.
	if sum > pay.AmountCents-pay.AllocatedCents {
		return nil, fmt.Errorf("allocation effect %s: %d cents over %d left: %w", g.GateID, sum, pay.AmountCents-pay.AllocatedCents, ErrInvalid)
	}
	return out, nil
}

// PGAllocations is AllocationStore over billing.payment and billing.payment_allocation.
type PGAllocations struct{}

var _ AllocationStore = PGAllocations{}

const lockAllocPaymentSQL = `SELECT p.id::text, coalesce(p.payer_contact_id::text, ''), p.amount_cents, p.deposit_to, p.status,
  coalesce((SELECT sum(a.amount_cents) FROM billing.payment_allocation a WHERE a.payment_id = p.id), 0)::bigint
FROM billing.payment p WHERE p.id = $1::uuid FOR UPDATE OF p`

const appliedUnderSQL = `SELECT EXISTS (SELECT 1 FROM billing.payment_allocation WHERE approval_id = $1::uuid)`

const insertApprovedAllocationSQL = `INSERT INTO billing.payment_allocation (payment_id, target_type, bill_id, client_id,
  amount_cents, fee_cents, tax_cents, allocated_by, approval_id)
VALUES ($1::uuid, CASE WHEN $2 = '' THEN 'client_credit' ELSE 'bill' END, NULLIF($2, '')::uuid,
  CASE WHEN $2 = '' THEN $3::uuid END, $4, $5, $6, $7::uuid, $8::uuid) RETURNING id::text`

// settleBillSQL moves an open bill to paid when nothing is left on it, else partially_paid.
const settleBillSQL = `UPDATE billing.bill b SET status = CASE WHEN bb.balance_cents <= 0 THEN 'paid' ELSE 'partially_paid' END
FROM billing.bill_balance bb WHERE bb.bill_id = b.id AND b.id = $1::uuid AND b.status IN ('delivered','partially_paid')`

// LockPayment implements AllocationStore.
func (PGAllocations) LockPayment(ctx context.Context, tx pgx.Tx, id string) (AllocPayment, error) {
	var p AllocPayment
	err := tx.QueryRow(ctx, lockAllocPaymentSQL, id).Scan(&p.ID, &p.ClientID, &p.AmountCents, &p.DepositTo, &p.Status, &p.AllocatedCents)
	if missing(err) {
		return AllocPayment{}, fmt.Errorf("payment %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return AllocPayment{}, fmt.Errorf("lock payment %s: %w", id, err)
	}
	return p, nil
}

// AppliedUnder implements AllocationStore.
func (PGAllocations) AppliedUnder(ctx context.Context, tx pgx.Tx, gateID string) (bool, error) {
	var ok bool
	if err := tx.QueryRow(ctx, appliedUnderSQL, gateID).Scan(&ok); err != nil {
		return false, fmt.Errorf("allocations under %s: %w", gateID, err)
	}
	return ok, nil
}

// LockBill implements AllocationStore.
func (PGAllocations) LockBill(ctx context.Context, tx pgx.Tx, id string) (AllocBill, error) {
	b, err := lockBill(ctx, tx, id, "")
	if err != nil {
		return AllocBill{}, err
	}
	return AllocBill{PayerID: b.payer, Status: b.status, OpenCents: b.total, OpenTax: b.tax}, nil
}

// Insert implements AllocationStore.
func (PGAllocations) Insert(ctx context.Context, tx pgx.Tx, a AppliedAllocation) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, insertApprovedAllocationSQL, a.PaymentID, a.BillID, a.ClientID, a.Total(), a.FeeCents,
		a.TaxCents, a.By, a.ApprovalID).Scan(&id); err != nil {
		return "", fmt.Errorf("allocate %s: %w", a.PaymentID, err)
	}
	return id, nil
}

// Settle implements AllocationStore.
func (PGAllocations) Settle(ctx context.Context, tx pgx.Tx, billID string) error {
	if _, err := tx.Exec(ctx, settleBillSQL, billID); err != nil {
		return fmt.Errorf("settle bill %s: %w", billID, err)
	}
	return nil
}
