package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// MethodCash is the Capture.Method of a cash payment.
const MethodCash = "cash"

// ErrCashUnchecked is returned for a cash payment when no cash-limit check is configured: an
// operating cash payment never goes in unchecked.
var ErrCashUnchecked = errors.New("payment: cash limit not checked")

// CashLimit is the By-Law 9 cash limit on one client file. trust.LedgerStore implements it,
// so trust cash receipts and operating cash payments share one limit and one per-matter lock.
type CashLimit interface {
	CheckOperatingCash(ctx context.Context, tx pgx.Tx, matterID string, amountCents int64, exceptionCode, gateID string) error
}

// CheckCash runs the cash limit for c in the capture's tx. Payments other than cash pass; a
// cash payment with no CashLimit or no matter fails closed.
func CheckCash(ctx context.Context, tx pgx.Tx, lim CashLimit, c Capture) error {
	if c.Method != MethodCash {
		return nil
	}
	// rule: BIL-L-039 (verify)
	if lim == nil {
		return fmt.Errorf("cash payment: %w", ErrCashUnchecked)
	}
	if c.MatterID == "" {
		return fmt.Errorf("cash payment has no matter: %w", ErrInvalid)
	}
	if err := lim.CheckOperatingCash(ctx, tx, c.MatterID, c.AmountCents, c.CashExceptionCode, c.CashGateID); err != nil {
		return fmt.Errorf("cash payment: %w", err)
	}
	return nil
}
