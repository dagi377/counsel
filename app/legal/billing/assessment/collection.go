package assessment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/interest"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/flags"
)

// FlagSolicitorsActVerified gates the Solicitors Act collection hold (V-BILL). It mirrors
// app/legal.FlagSolicitorsActVerified, which this package cannot import.
const FlagSolicitorsActVerified = "billing.solicitors_act_verified"

// Collection hold errors.
var (
	// ErrTooEarly means a fee suit may not start yet (409 TOO_EARLY).
	ErrTooEarly = errors.New("assessment: collection action too early")
	// ErrCollectionUnverified means the Solicitors Act flag is off, so fee suits stay disabled.
	ErrCollectionUnverified = errors.New("assessment: collection actions disabled until Solicitors Act rules are verified")
)

// CollectionAction is the type of a collections action.
type CollectionAction string

// Collection action types. Only legal actions are held.
const (
	ActionLegal         CollectionAction = "legal_action"
	ActionClaimReferral CollectionAction = "statement_of_claim_referral"
	ActionReminder      CollectionAction = "reminder"
)

// CollectionHold decides when a fee suit on a delivered bill may start.
type CollectionHold struct {
	Calendar interest.Calendar // the one shared months_after
	Clock    clock.Clock
	Flags    flags.Flags
}

// EarliestCollection is the first business date a fee suit may start: the day after
// months_after(delivery date in the business zone, 1).
func (h CollectionHold) EarliestCollection(deliveredAt time.Time) time.Time {
	loc := h.Clock.Location()
	y, m, d := deliveredAt.In(loc).Date()
	// rule: BIL-L-004 (verify)
	return h.Calendar.MonthsAfter(time.Date(y, m, d, 0, 0, 0, 0, loc), 1).AddDate(0, 0, 1)
}

// Check returns nil when action may start today on a bill delivered at deliveredAt.
func (h CollectionHold) Check(ctx context.Context, action CollectionAction, deliveredAt time.Time) error {
	if action != ActionLegal && action != ActionClaimReferral {
		return nil
	}
	if !h.Flags.Enabled(FlagSolicitorsActVerified) {
		return ErrCollectionUnverified
	}
	t := h.Clock.Today(ctx)
	today := time.Date(t.Year, t.Month, t.Day, 0, 0, 0, 0, h.Clock.Location())
	earliest := h.EarliestCollection(deliveredAt)
	// rule: BIL-L-004 (verify)
	if today.Before(earliest) {
		return fmt.Errorf("earliest %s: %w", earliest.Format(time.DateOnly), ErrTooEarly)
	}
	return nil
}
