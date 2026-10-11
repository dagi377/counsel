package bill

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// PGFeesBook is the fees book over Postgres. Numbered billing.bill rows are the register and
// the billing.fees_book view (migrations/5600_fees_book_view.sql) is the book, so approval
// writes no separate row: Append only checks that the approved bill shows in the view
// inside the gate's tx.
type PGFeesBook struct{}

var _ FeesBookWriter = PGFeesBook{}

// Append checks that e's bill is in the fees book view within tx.
//
// rule: BIL-L-040 (verify)
func (PGFeesBook) Append(ctx context.Context, tx pgx.Tx, e FeesBookEntry) error {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM billing.fees_book WHERE number = $1`, e.Number).Scan(&n); err != nil {
		return fmt.Errorf("fees book %s: %w", e.Number, err)
	}
	if n != 1 {
		return fmt.Errorf("fees book %s: %d rows in view: %w", e.Number, n, ErrNotApproved)
	}
	return nil
}

// Export reads the fees book view for bills dated in [from, to] (business dates) and returns
// it through ExportFeesBook: number order, no gaps, fail closed while the flag is off.
//
// rule: BIL-L-040 (verify)
func (PGFeesBook) Export(ctx context.Context, tx pgx.Tx, from, to time.Time) ([]FeesBookEntry, error) {
	lo, hi := businessDate(from), businessDate(to)
	rows, err := tx.Query(ctx, `
SELECT number, bill_date, client_id::text, matter_id::text, fees_cents, disbursements_cents,
       discount_cents, tax_cents, total_cents, status
  FROM billing.fees_book
 WHERE bill_date BETWEEN $1::date AND $2::date`, lo.Format(time.DateOnly), hi.Format(time.DateOnly))
	if err != nil {
		return nil, fmt.Errorf("export fees book: %w", err)
	}
	defer rows.Close()
	var entries []FeesBookEntry
	for rows.Next() {
		var e FeesBookEntry
		var d time.Time
		var st string
		if err := rows.Scan(&e.Number, &d, &e.ClientID, &e.MatterID, &e.FeesCents, &e.DisbursementsCents,
			&e.DiscountCents, &e.TaxCents, &e.TotalCents, &st); err != nil {
			return nil, fmt.Errorf("export fees book: %w", err)
		}
		e.BillDate = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, business)
		e.Status = FeesBookStatus(st)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("export fees book: %w", err)
	}
	return ExportFeesBook(ctx, entries, from, to)
}
