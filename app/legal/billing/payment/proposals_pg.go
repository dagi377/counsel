package payment

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/server"
)

// PGProposals is Proposals over billing.payment_allocation_proposal (5630). A set touching
// operating bills only is saved as proposed (HTTP-15); applying it is the
// billing.payment_allocation effect's, so nothing here writes billing.payment_allocation.
type PGProposals struct{}

var _ Proposals = PGProposals{}

const lockProposedPaymentSQL = `SELECT coalesce(p.payer_contact_id::text, ''), p.amount_cents, p.status
FROM billing.payment p WHERE p.id = $1::uuid AND ` + operating + ` FOR UPDATE OF p`

const supersedeProposalSQL = `UPDATE billing.payment_allocation_proposal SET status = 'superseded'
WHERE payment_id = $1::uuid AND status = 'proposed'`

const insertProposalSQL = `INSERT INTO billing.payment_allocation_proposal
  (payment_id, ordinal, bill_id, amount_cents, fee_cents, tax_cents, proposed_by)
VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7::uuid)`

// Propose implements Proposals: it replaces the payment's proposed set with rows, each split
// into fee and tax as Capture splits it, in the caller's tx.
func (PGProposals) Propose(ctx context.Context, tx pgx.Tx, paymentID string, version int, rows []ProposedAllocation, by string) ([]Allocation, error) {
	// rule: ARC-I-006
	if version != paymentVersion {
		return nil, fmt.Errorf("propose %s: version %d: %w", paymentID, version, server.ErrPreconditionFailed)
	}
	if err := checkProposed(rows, by); err != nil {
		return nil, fmt.Errorf("propose %s: %w", paymentID, err)
	}
	var payer, status string
	var amount int64
	err := tx.QueryRow(ctx, lockProposedPaymentSQL, paymentID).Scan(&payer, &amount, &status)
	if missing(err) {
		return nil, fmt.Errorf("propose %s: %w", paymentID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("propose %s: %w", paymentID, err)
	}
	if status == statusReversed {
		return nil, fmt.Errorf("propose %s: %w: %w", paymentID, ErrInvalid, ErrReversed)
	}
	var sum int64
	for _, r := range rows {
		sum += r.AmountCents
	}
	// rule: DM-045
	// A proposed set never exceeds the payment.
	if sum > amount {
		return nil, fmt.Errorf("propose %s: %d cents over a %d cent payment: %w", paymentID, sum, amount, ErrInvalid)
	}
	out := make([]Allocation, 0, len(rows))
	for _, r := range rows {
		b, err := lockBill(ctx, tx, r.TargetID, paymentID)
		if err != nil {
			return nil, fmt.Errorf("propose %s: %w", paymentID, err)
		}
		if err := b.payable(payer); err != nil {
			return nil, fmt.Errorf("propose %s: bill %s: %w", paymentID, r.TargetID, err)
		}
		// rule: DM-047
		// A proposed row never pays more than is open on its bill.
		if r.AmountCents > b.total {
			return nil, fmt.Errorf("propose %s: bill %s has %d cents open: %w", paymentID, r.TargetID, b.total, ErrInvalid)
		}
		// rule: ARC-I-029
		// The bucket split is computed here, never taken from the request.
		fee, tax, err := b.split(r.AmountCents)
		if err != nil {
			return nil, fmt.Errorf("propose %s: %w", paymentID, err)
		}
		out = append(out, Allocation{PaymentID: paymentID, BillID: r.TargetID, FeeCents: fee, TaxCents: tax})
	}
	if _, err := tx.Exec(ctx, supersedeProposalSQL, paymentID); err != nil {
		return nil, fmt.Errorf("propose %s: supersede: %w", paymentID, err)
	}
	for i, a := range out {
		if _, err := tx.Exec(ctx, insertProposalSQL, paymentID, i, a.BillID, a.Total(), a.FeeCents, a.TaxCents, by); err != nil {
			return nil, fmt.Errorf("propose %s: row %d: %w", paymentID, i, err)
		}
	}
	return out, nil
}

// checkProposed accepts a non-empty set of positive rows, each naming a different bill.
func checkProposed(rows []ProposedAllocation, by string) error {
	if by == "" || len(rows) == 0 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, r := range rows {
		// rule: DM-046
		// Trust ledgers and unapplied credit go through billing.payment_allocation, never here.
		if r.TargetType != "bill" || r.TargetID == "" || seen[r.TargetID] || r.AmountCents <= 0 {
			return ErrInvalid
		}
		seen[r.TargetID] = true
	}
	return nil
}
