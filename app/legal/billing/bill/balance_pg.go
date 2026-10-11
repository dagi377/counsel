package bill

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// FeeBillState is what a fee transfer out of trust needs from the bill it pays.
type FeeBillState struct {
	// DeliveredAt is zero while the bill is undelivered or void.
	DeliveredAt time.Time
	// BalanceDueCents is the bill balance net of fee transfers already authorized or executed
	// against it (billing.bill_balance balance_cents - reserved_cents). It may be negative.
	BalanceDueCents int64
}

// PGBalances reads bill balances from the billing.bill_balance view, the one balance formula
// (total - paid - credited - written off - applied fee transfers + interest).
type PGBalances struct {
	DB db.DB
}

const balanceSQL = `SELECT b.status, bb.balance_cents
  FROM billing.bill b JOIN billing.bill_balance bb ON bb.bill_id = b.id
  WHERE b.id = $1::uuid`

// BalanceCents returns what is still owed on bill id, in cents, signed: an overpaid bill is
// negative. A void bill owes nothing. An unknown bill is ErrNotFound.
func (p PGBalances) BalanceCents(ctx context.Context, id ID) (int64, error) {
	if p.DB == nil {
		return 0, fmt.Errorf("bill balance %s: no database: %w", id, ErrNotFound)
	}
	var (
		status  Status
		balance int64
	)
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, balanceSQL, string(id)).Scan(&status, &balance)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("bill balance %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("bill balance %s: %w", id, err)
	}
	if status == StatusVoid {
		return 0, nil
	}
	return balance, nil
}

const feeBillSQL = `SELECT b.status, b.delivered_at, bb.balance_cents - bb.reserved_cents
  FROM billing.bill b JOIN billing.bill_balance bb ON bb.bill_id = b.id
  WHERE b.id = $1::uuid AND b.matter_id = $2::uuid`

// FeeBill reads the delivery and balance due of billID, which must be a bill on matterID; any
// other bill is ErrNotFound. A void bill reads as undelivered, so no fee may be paid on it.
func (p PGBalances) FeeBill(ctx context.Context, matterID, billID string) (FeeBillState, error) {
	if p.DB == nil {
		return FeeBillState{}, fmt.Errorf("fee bill %s: no database: %w", billID, ErrNotFound)
	}
	if matterID == "" || billID == "" {
		return FeeBillState{}, fmt.Errorf("fee bill %q on matter %q: %w", billID, matterID, ErrNotFound)
	}
	var (
		status    Status
		delivered *time.Time
		out       FeeBillState
	)
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, feeBillSQL, billID, matterID).Scan(&status, &delivered, &out.BalanceDueCents)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return FeeBillState{}, fmt.Errorf("fee bill %s on matter %s: %w", billID, matterID, ErrNotFound)
	}
	if err != nil {
		return FeeBillState{}, fmt.Errorf("fee bill %s: %w", billID, err)
	}
	if delivered != nil && status != StatusVoid {
		out.DeliveredAt = *delivered
	}
	return out, nil
}
