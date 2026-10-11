package interest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
)

// FlagSolicitorsActVerified gates interest accrual (V-BILL, off by default). It mirrors
// app/legal.FlagSolicitorsActVerified, which this package cannot import.
const FlagSolicitorsActVerified = legalflags.SolicitorsActVerified

// FlagCJADefaultRate (verify, off by default) applies the Courts of Justice Act s.127
// prejudgment rate for the quarter of delivery to a bill with no agreed rate (Solicitors Act
// s.33, Q-INTEREST-CJA). Off, a bill with no agreed rate never accrues interest.
const FlagCJADefaultRate = legalflags.InterestCJADefaultRate

// Accrual start errors.
var (
	// ErrAccrualUnverified means the Solicitors Act flag is off, so no interest accrues.
	ErrAccrualUnverified = errors.New("interest: accrual disabled until Solicitors Act rules are verified")
	// ErrNotDelivered means the bill has no delivery date, so no interest clock has started.
	ErrNotDelivered = errors.New("interest: bill not delivered")
)

// Start decides the first business date interest may accrue on one bill.
type Start struct {
	Calendar Calendar // the one shared months_after
}

// From returns the first business date interest accrues on b: the later of due (a
// business date; zero means none) and Calendar.AccrualStart(b.DeliveredAt).
func (s Start) From(ctx context.Context, b Bill, due time.Time) (time.Time, error) {
	if !legalflags.Enabled(ctx, FlagSolicitorsActVerified) {
		return time.Time{}, ErrAccrualUnverified
	}
	// rule: BIL-C-071
	// rule: BIL-L-009
	if _, err := EffectiveRate(ctx, b); err != nil {
		return time.Time{}, err
	}
	// rule: BIL-L-012
	if b.DeliveredAt.IsZero() {
		return time.Time{}, fmt.Errorf("bill %s: %w", b.ID, ErrNotDelivered)
	}
	// rule: BIL-C-073
	// rule: BIL-L-008 (verify one-month computation)
	start := s.Calendar.AccrualStart(b.DeliveredAt)
	// rule: BIL-C-070
	if !due.IsZero() {
		y, m, d := due.Date()
		if dd := time.Date(y, m, d, 0, 0, 0, 0, start.Location()); dd.After(start) {
			start = dd
		}
	}
	return start, nil
}

// Accrues reports whether interest accrues on the business date day. Any error from From
// means no interest for that day.
// rule: BIL-I-023
func (s Start) Accrues(ctx context.Context, b Bill, due, day time.Time) bool {
	start, err := s.From(ctx, b, due)
	if err != nil {
		return false
	}
	y, m, d := day.Date()
	return !time.Date(y, m, d, 0, 0, 0, 0, start.Location()).Before(start)
}

// Clip returns the part of the half-open period p on which interest accrues. A posting
// (BIL-C-072) only ever covers days on or after the start.
// rule: BIL-C-072
func (s Start) Clip(ctx context.Context, b Bill, due time.Time, p Period) (Period, error) {
	if !p.From.Before(p.To) {
		return Period{}, fmt.Errorf("bill %s: %w", b.ID, ErrBadPeriod)
	}
	start, err := s.From(ctx, b, due)
	if err != nil {
		return Period{}, err
	}
	if !p.To.After(start) {
		return Period{}, fmt.Errorf("bill %s: %w", b.ID, ErrNotDue)
	}
	if p.From.Before(start) {
		p.From = start
	}
	return p, nil
}
