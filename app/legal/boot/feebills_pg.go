package boot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/trust/withdraw"
)

// ErrFeeBillNotFound is returned when a fee withdrawal names an unknown bill.
var ErrFeeBillNotFound = errors.New("legal boot: fee bill not found")

// PGFeeBills is withdraw.FeeBills over billing.bill and billing.bill_balance: the bill's
// matter, delivery time and unpaid balance net of fee transfers already reserved (authorised
// or executed, not yet applied). The bill row is locked so two fee withdrawals on one bill
// are authorised one after the other.
type PGFeeBills struct{}

var _ withdraw.FeeBills = PGFeeBills{}

const feeBillSQL = `SELECT b.matter_id::text, b.delivered_at, (bb.balance_cents - bb.reserved_cents)::bigint
FROM billing.bill b JOIN billing.bill_balance bb ON bb.bill_id = b.id
WHERE b.id = $1::uuid FOR UPDATE OF b`

// FeeBill implements withdraw.FeeBills.
func (PGFeeBills) FeeBill(ctx context.Context, tx pgx.Tx, billID string) (withdraw.FeeBill, error) {
	var (
		out withdraw.FeeBill
		at  *time.Time
	)
	err := tx.QueryRow(ctx, feeBillSQL, billID).Scan(&out.MatterID, &at, &out.BalanceDueCents)
	var pg *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
		return withdraw.FeeBill{}, fmt.Errorf("fee bill %s: %w", billID, ErrFeeBillNotFound)
	}
	if err != nil {
		return withdraw.FeeBill{}, fmt.Errorf("fee bill %s: %w", billID, err)
	}
	if at != nil {
		out.DeliveredAt = *at
	}
	return out, nil
}
