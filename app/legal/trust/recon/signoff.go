package recon

import (
	"context"
	"fmt"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/kernel/clock"
)

// Locker reports whether an account's month is reconciled and locked. Service satisfies it.
type Locker interface {
	Locked(ctx context.Context, accountID string, p Period) (bool, error)
}

// PeriodOf returns the calendar month that contains the business date d.
func PeriodOf(d clock.Date) Period {
	return Period{Year: d.Year, Month: d.Month}
}

// SignOff approves a prepared reconciliation, which locks its month for the account. The
// approver must not be the preparer and must not be an agent; the service enforces both.
// After approval the month must report locked, otherwise the sign-off is refused.
func SignOff(ctx context.Context, svc Service, id string, by Approver) error {
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return fmt.Errorf("sign off %s: %w", id, ErrUnverified)
	}
	if by.IsAgent {
		return fmt.Errorf("sign off %s: %w", id, ErrAgentApproval)
	}
	if err := svc.Approve(ctx, id, by); err != nil {
		return fmt.Errorf("sign off: %w", err)
	}
	r, err := svc.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("sign off: %w", err)
	}
	// rule: BIL-I-025
	locked, err := svc.Locked(ctx, r.AccountID, r.Period)
	if err != nil {
		return fmt.Errorf("sign off %s: lock: %w", id, err)
	}
	if !locked {
		return fmt.Errorf("sign off %s: month not locked after approval: %w", id, ErrInvalidState)
	}
	return nil
}

// CheckPostingDate refuses a trust posting dated in a month whose reconciliation is signed.
// A correction to a locked month posts in the current period instead. While
// FlagBylaw9Verified is off the check fails closed with ErrUnverified.
func CheckPostingDate(ctx context.Context, l Locker, accountID string, d clock.Date) error {
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return fmt.Errorf("check posting date: %w", ErrUnverified)
	}
	p := PeriodOf(d)
	locked, err := l.Locked(ctx, accountID, p)
	if err != nil {
		return fmt.Errorf("check posting date: %w", err)
	}
	// rule: BIL-I-025
	if locked {
		return fmt.Errorf("check posting date %s: month %d-%02d: %w", d, p.Year, int(p.Month), ErrPeriodLocked)
	}
	return nil
}
