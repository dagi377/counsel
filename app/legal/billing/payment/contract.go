// CONTRACT: app/legal/billing/payment. Exported shapes here are frozen after wave 0; changes
// are additive only and need a docs/QUESTIONS.md entry. Bodies live in the T-021 subtasks.

// Package payment captures operating payments against bills, splits each payment between
// the fee and tax buckets, holds overpayments as unapplied, and reverses payments (NSF,
// refund, chargeback) bucket for bucket.
package payment

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned for an unknown payment or bill.
	ErrNotFound = errors.New("payment: not found")
	// ErrInvalid is returned for a non-positive amount or missing fields (422).
	ErrInvalid = errors.New("payment: invalid payment")
	// ErrRetainerToOperating is returned when retainer money is received into operating.
	ErrRetainerToOperating = errors.New("payment: retainer received into operating")
	// ErrOtherClient is returned when a payment or credit is applied to another client's bill.
	ErrOtherClient = errors.New("payment: bill belongs to another client")
	// ErrReversed is returned when a reversed payment is reversed or applied again.
	ErrReversed = errors.New("payment: already reversed")
)

// Kind says what a received payment is for.
type Kind string

// Kinds.
const (
	KindBillPayment Kind = "bill_payment"
	KindRetainer    Kind = "retainer"
)

// Status is a payment's state.
type Status string

// Statuses.
const (
	StatusCaptured Status = "captured"
	StatusReversed Status = "reversed"
)

// ReversalReason says why a payment was reversed.
type ReversalReason string

// Reversal reasons.
const (
	ReasonNSF        ReversalReason = "nsf"
	ReasonRefund     ReversalReason = "refund"
	ReasonChargeback ReversalReason = "chargeback"
)

// Capture is one payment received into operating. Provider and ProviderEventID dedup
// provider webhooks; a manual payment leaves them empty and uses IdempotencyKey.
type Capture struct {
	ClientID        string
	BillID          string
	Kind            Kind
	AmountCents     int64
	ReceivedOn      time.Time // business date in America/Toronto
	Provider        string
	ProviderEventID string
	IdempotencyKey  string
	RecordedBy      string
}

// Payment is a captured payment.
type Payment struct {
	ID      string
	Capture Capture
	Status  Status
	Reason  ReversalReason
}

// Allocation is the part of a payment applied to one bill, split by bucket. A row with an
// empty BillID is unapplied money (overpayment) awaiting credit, refund or transfer.
type Allocation struct {
	PaymentID string
	BillID    string
	FeeCents  int64
	TaxCents  int64
}

// Total is the cents the allocation moves.
func (a Allocation) Total() int64 { return a.FeeCents + a.TaxCents }

// Service is the payment surface other tasks call.
type Service interface {
	// Capture records a payment and allocates it to the bill: the tax share is
	// rhu(amount*open_tax, open_total) and the rest goes to fees; any excess is unapplied.
	// The same (provider, event_id) or idempotency key returns the first payment.
	Capture(ctx context.Context, c Capture) (Payment, []Allocation, error)
	// Reverse undoes a payment, restoring each allocation's bucket split exactly.
	Reverse(ctx context.Context, paymentID string, reason ReversalReason) (Payment, error)
	// Get returns one payment or ErrNotFound.
	Get(ctx context.Context, paymentID string) (Payment, error)
	// Allocations lists a payment's allocations in the order they were made.
	Allocations(ctx context.Context, paymentID string) ([]Allocation, error)
}
