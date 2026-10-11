package credit

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PGBills is Bills over billing.bill and billing.bill_balance, the slice of a bill POST
// /credit-notes checks before it opens the credit-note gate. The read runs in its own RLS
// transaction; the gate effect re-reads the bill under lock before anything is issued.
type PGBills struct {
	DB TxRunner
	// Lawyers, when set, fills Bill.ResponsibleUser through the matter module's reader; a
	// failed lookup fails the read. When nil ResponsibleUser stays empty: the credit-note
	// request does not use it.
	Lawyers ResponsibleLawyers
}

var _ Bills = PGBills{}

// billSQL reads a bill's delivery and open balance (fee plus tax, net of reserved fee
// transfers). A void bill is never delivered.
const billSQL = `SELECT b.id::text, b.matter_id::text, b.delivered_at IS NOT NULL AND b.status <> 'void',
  bb.balance_cents - bb.reserved_cents
  FROM billing.bill b JOIN billing.bill_balance bb ON bb.bill_id = b.id
  WHERE b.id = $1::uuid`

// Bill implements Bills. An unknown or malformed id is ErrNotFound. The open balance is the
// stored figure as it is: a negative balance is reported, never clamped to zero, so every
// credit against it is refused as exceeding the balance.
func (p PGBills) Bill(ctx context.Context, id string) (Bill, error) {
	if p.DB == nil {
		return Bill{}, fmt.Errorf("bill %s: no database: %w", id, ErrInvalidState)
	}
	var b Bill
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var matter string
		err := tx.QueryRow(ctx, billSQL, id).Scan(&b.ID, &matter, &b.Delivered, &b.OpenCents)
		if notFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if p.Lawyers == nil {
			return nil
		}
		lawyer, err := p.Lawyers.ResponsibleLawyer(ctx, tx, matter)
		if err != nil {
			return fmt.Errorf("responsible lawyer: %w", err)
		}
		b.ResponsibleUser = lawyer
		return nil
	})
	if err != nil {
		return Bill{}, fmt.Errorf("bill %s: %w", id, err)
	}
	return b, nil
}
