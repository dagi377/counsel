package payment

import (
	"errors"
	"fmt"
	"time"
)

// Overpayment errors.
var (
	// ErrCreditState is returned when a credit step does not fit the credit's state (409).
	ErrCreditState = errors.New("payment: client credit is not in a state for this step")
	// ErrWrongGate is returned when a credit step is approved under the wrong gate.
	ErrWrongGate = errors.New("payment: wrong gate for this client credit step")
)

// Gates that move overpaid money (BIL-C-063, BIL-C-067).
const (
	GateCredit   = "G-B3" // unapplied -> client credit, and credit applied to a bill
	GateRefund   = "G-B6" // client credit refunded
	GateTransfer = "G6"   // client credit paid into trust
)

// AccountClientCredit is the operating ledger account that holds client credit.
const AccountClientCredit = "OP-2200"

// ExceptionClientMoney is the title of the task the aging job opens for stale credit.
const ExceptionClientMoney = "client money in operating"

// CreditState is where overpaid money sits.
type CreditState string

// Credit states.
const (
	CreditUnapplied   CreditState = "unapplied" // G-B3 pending: an unapplied allocation
	CreditHeld        CreditState = "credit"    // OP-2200 client credit
	CreditApplied     CreditState = "applied"
	CreditRefunded    CreditState = "refunded"
	CreditTransferred CreditState = "transferred_to_trust"
)

// Credit is overpaid money of one client from one payment.
type Credit struct {
	PaymentID   string
	ClientID    string
	AmountCents int64
	CreatedOn   time.Time // business date the payment was received, America/Toronto
	State       CreditState
	Account     string // AccountClientCredit once held
}

// Overpayment returns the unapplied part of a payment's allocation rows as a credit awaiting
// G-B3. ok is false when the payment was fully applied to bills.
func Overpayment(paymentID, clientID string, receivedOn time.Time, rows []Row) (c Credit, ok bool) {
	var left int64
	for _, r := range rows {
		if r.BillID == "" {
			left += r.AmountCents
		}
	}
	if left <= 0 {
		return Credit{}, false
	}
	// rule: BIL-C-063
	// Excess is an unapplied allocation while G-B3 is pending.
	return Credit{PaymentID: paymentID, ClientID: clientID, AmountCents: left, CreatedOn: receivedOn, State: CreditUnapplied}, true
}

// Hold moves unapplied money to client credit once gate G-B3 approves.
func (c Credit) Hold(gate string) (Credit, error) {
	if gate != GateCredit {
		return c, fmt.Errorf("hold client credit under %q: %w", gate, ErrWrongGate)
	}
	if c.State != CreditUnapplied {
		return c, fmt.Errorf("hold client credit from %s: %w", c.State, ErrCreditState)
	}
	// rule: BIL-C-063
	// Approved excess becomes ClientCredit (OP-2200) for that client.
	c.State, c.Account = CreditHeld, AccountClientCredit
	return c, nil
}

// ApplyTo applies the whole credit to bill b under G-B3. b must be a delivered bill of the
// same client.
func (c Credit) ApplyTo(b OpenBill, gate string) (Credit, error) {
	if gate != GateCredit {
		return c, fmt.Errorf("apply client credit under %q: %w", gate, ErrWrongGate)
	}
	if c.State != CreditHeld {
		return c, fmt.Errorf("apply client credit from %s: %w", c.State, ErrCreditState)
	}
	// rule: BIL-C-063
	// Never to another client.
	if b.ClientID != c.ClientID {
		return c, fmt.Errorf("apply client credit to bill %s: %w", b.ID, ErrOtherClient)
	}
	if b.DeliveredAt.IsZero() {
		return c, fmt.Errorf("apply client credit to bill %s: %w", b.ID, ErrNotDelivered)
	}
	c.State = CreditApplied
	return c, nil
}

// Refund marks the credit refunded under G-B6. CheckRefund validates the approval itself.
func (c Credit) Refund(gate string) (Credit, error) {
	return c.close(gate, GateRefund, CreditRefunded)
}

// ToTrust marks the credit paid into trust under G6.
func (c Credit) ToTrust(gate string) (Credit, error) {
	return c.close(gate, GateTransfer, CreditTransferred)
}

func (c Credit) close(gate, want string, to CreditState) (Credit, error) {
	if gate != want {
		return c, fmt.Errorf("%s client credit under %q: %w", to, gate, ErrWrongGate)
	}
	if c.State != CreditHeld {
		return c, fmt.Errorf("%s client credit from %s: %w", to, c.State, ErrCreditState)
	}
	c.State = to
	return c, nil
}

// Exception is a task the aging job opens for client money left in operating.
type Exception struct {
	PaymentID string
	ClientID  string
	Title     string
}

// AgeCredits returns an exception for each credit still held past maxDays business days
// (0 means DefaultCreditMaxBusinessDays). businessDays counts business days between dates.
func AgeCredits(credits []Credit, today time.Time, maxDays int, businessDays func(from, to time.Time) int) []Exception {
	var out []Exception
	for _, c := range credits {
		if c.State != CreditHeld && c.State != CreditUnapplied {
			continue
		}
		// rule: BIL-C-063
		// Client credit is short-lived: untouched past the limit it raises an exception.
		if CreditOverdue(c.CreatedOn, today, maxDays, businessDays) {
			out = append(out, Exception{PaymentID: c.PaymentID, ClientID: c.ClientID, Title: ExceptionClientMoney})
		}
	}
	return out
}
