// CONTRACT: app/legal/billing/interest. Frozen once on main; changes are additive only and
// need a docs/QUESTIONS.md entry. Bodies live in the T-024 subtasks (start.go, calc.go,
// months.go, post.go).

// Package interest accrues simple interest on overdue bills at the rate agreed with the
// client, from one calendar month after delivery, and posts it as untaxed bill lines.
package interest

import (
	"context"
	"errors"
	"time"

	"github.com/dagi377/counsel/kernel/money"
)

// Sentinel errors.
var (
	// ErrNoAgreedRate is returned when a bill has no agreed interest rate: no interest, ever.
	ErrNoAgreedRate = errors.New("interest: no agreed rate")
	// ErrNotDue is returned when the period ends before interest starts to accrue.
	ErrNotDue = errors.New("interest: not yet accruing")
	// ErrAlreadyPosted is returned when the bill already has a charge for the same period start.
	ErrAlreadyPosted = errors.New("interest: already posted for period")
	// ErrBadPeriod is returned for an empty or reversed period.
	ErrBadPeriod = errors.New("interest: bad period")
)

// Bill is what accrual needs to know about one bill. Dates are business dates
// (America/Toronto); DeliveredAt is an instant.
type Bill struct {
	ID          string
	DeliveredAt time.Time   // when the bill was delivered (instant, any zone)
	Principal   money.Cents // unpaid fees and disbursements the interest runs on
	RatePPM     money.PPM   // rate snapshot agreed with the client; 0 means none agreed

	// Additive (Q-W3-LEGAL-INTEREST). DueDate is the agreed payment due date, a business
	// date; zero means none agreed. Interest starts on the later of DueDate and one month
	// after delivery (BIL-C-070).
	DueDate time.Time
}

// Period is a half-open range of business dates [From, To).
type Period struct {
	From time.Time
	To   time.Time
}

// Charge mirrors one billing.interest_charge row. It carries no tax.
type Charge struct {
	BillID    string
	Period    Period
	Principal money.Cents
	RatePPM   money.PPM
	Days      int
	Amount    money.Cents // money.Rhu(Principal*RatePPM*Days, 1e6*365)
}

// Calendar computes the accrual start date.
type Calendar interface {
	// MonthsAfter adds n calendar months to a business date, clamping to the month's
	// last day (2026-01-31 + 1 = 2026-02-28).
	MonthsAfter(d time.Time, n int) time.Time
	// AccrualStart is the first business date interest accrues on: MonthsAfter(delivery
	// date in America/Toronto, 1), per Solicitors Act s.33 (BIL-C-073, BIL-L-008; verify).
	AccrualStart(deliveredAt time.Time) time.Time
}

// Calculator computes simple interest, actual/365.
type Calculator interface {
	Simple(principal money.Cents, rate money.PPM, days int) (money.Cents, error)
}

// Poster records one interest charge per bill per period start.
type Poster interface {
	Post(ctx context.Context, b Bill, p Period) (Charge, error)
}

// Service is the whole interest surface other billing tasks call.
type Service interface {
	Calendar
	Calculator
	Poster
}
