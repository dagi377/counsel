package trustapply

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
)

// FlagBylaw9Verified gates trust-to-fees transfers until the By-Law 9 provisions are
// verified. It is off by default.
const FlagBylaw9Verified = legalflags.TrustBylaw9Verified

// Sentinel errors.
var (
	// ErrDisabled is returned while FlagBylaw9Verified is off.
	ErrDisabled = errors.New("trustapply: trust fee transfers are disabled")
	// ErrInvalid is returned for a non-positive amount or missing fields (422).
	ErrInvalid = errors.New("trustapply: invalid transfer")
	// ErrNotDelivered is returned for a bill not delivered on or before the transfer date
	// (409 not_delivered).
	ErrNotDelivered = errors.New("trustapply: bill not delivered")
	// ErrExceedsBill is returned when the amount exceeds the bill balance less reserved
	// transfers (409).
	ErrExceedsBill = errors.New("trustapply: amount exceeds bill balance")
	// ErrTrustOverdraw is returned when the amount exceeds the available trust balance
	// (409 trust_overdraw).
	ErrTrustOverdraw = errors.New("trustapply: trust overdraw")
)

// Bill is the slice of a bill a trust fee transfer reads.
type Bill struct {
	ID            string
	MatterID      string
	DeliveredOn   time.Time // business date of delivery; zero when undelivered
	BalanceCents  int64     // unpaid balance
	ReservedCents int64     // authorized or executed-unapplied fee transfers (BIL-C-054)
}

// Transfer is a proposed trust-to-fees transfer for one bill.
type Transfer struct {
	BillID         string
	TrustAccountID string
	MatterID       string
	AmountCents    int64
	On             time.Time // business date of the step, America/Toronto
}

// Store reads the rows a transfer step checks. Both reads lock their row
// (SELECT ... FOR UPDATE) in the caller's transaction.
type Store interface {
	// LockBill returns the bill, locked.
	LockBill(ctx context.Context, billID string) (Bill, error)
	// LockFunds locks the client ledger row and returns its trust position; Check takes
	// the available balance from it with withdraw.Funds.Available (BIL-C-053).
	LockFunds(ctx context.Context, trustAccountID, matterID string) (withdraw.Funds, error)
}

// Applier checks and applies trust fee transfers.
type Applier struct {
	Store Store
}

// Check validates t at proposal, authorization and execution. It returns the locked bill.
func (a Applier) Check(ctx context.Context, t Transfer) (Bill, error) {
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return Bill{}, ErrDisabled
	}
	if t.AmountCents <= 0 || t.BillID == "" || t.TrustAccountID == "" || t.MatterID == "" {
		return Bill{}, fmt.Errorf("trust fee transfer: %w", ErrInvalid)
	}
	b, err := a.Store.LockBill(ctx, t.BillID)
	if err != nil {
		return Bill{}, fmt.Errorf("lock bill %s: %w", t.BillID, err)
	}
	// rule: BIL-L-035
	// Trust pays fees only for a bill delivered on or before the transfer date.
	// rule: BIL-I-011
	// A trust-to-fees transfer exists only for a delivered bill.
	if b.DeliveredOn.IsZero() || b.DeliveredOn.After(t.On) {
		return Bill{}, fmt.Errorf("trust fee transfer for bill %s: %w", b.ID, ErrNotDelivered)
	}
	// rule: BIL-C-050
	// For a combined bill only the lines of the transfer's matter are payable from it.
	if b.MatterID != t.MatterID {
		return Bill{}, fmt.Errorf("trust fee transfer for bill %s: %w", b.ID, ErrInvalid)
	}
	// rule: BIL-C-054
	// A new transfer needs amount <= balance - reserved_cents.
	// rule: BIL-I-011
	// Authorized + executed-unapplied transfers never exceed the bill balance.
	if t.AmountCents > b.BalanceCents-b.ReservedCents {
		return Bill{}, fmt.Errorf("trust fee transfer for bill %s: %w", b.ID, ErrExceedsBill)
	}
	// rule: BIL-C-051
	// The client ledger row is locked and must have available >= amount, so concurrent
	// authorizations against the same funds serialize and the second fails.
	funds, err := a.Store.LockFunds(ctx, t.TrustAccountID, t.MatterID)
	if err != nil {
		return Bill{}, fmt.Errorf("lock trust ledger: %w", err)
	}
	avail := funds.Available()
	// rule: BIL-C-050
	// amount <= min(bill balance - reserved, trust available).
	if t.AmountCents > avail {
		return Bill{}, fmt.Errorf("trust fee transfer for bill %s: %w", b.ID, ErrTrustOverdraw)
	}
	return b, nil
}

// Application is how an executed transfer lands on its bill.
type Application struct {
	AppliedCents    int64     // paid to the bill
	DueToTrustCents int64     // excess posted to OP-2300 Due to trust
	ReturnTaskDue   time.Time // G6 return-to-trust task due date; zero when no excess
}

// Apply applies an executed transfer of amount to a bill whose balance is now balance.
func Apply(amount, balance int64, today time.Time) (Application, error) {
	if amount <= 0 || balance < 0 {
		return Application{}, fmt.Errorf("apply trust fee transfer: %w", ErrInvalid)
	}
	// rule: BIL-C-055
	// Excess over the bill balance at application goes to OP-2300 with a G6
	// return-to-trust task due the same day.
	if amount <= balance {
		return Application{AppliedCents: amount}, nil
	}
	return Application{AppliedCents: balance, DueToTrustCents: amount - balance, ReturnTaskDue: today}, nil
}
