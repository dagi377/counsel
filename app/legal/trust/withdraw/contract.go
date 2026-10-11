// CONTRACT: app/legal/trust/withdraw. Exported shapes here are frozen after wave 0; changes
// are additive only and need a docs/QUESTIONS.md entry. Bodies live in the T-012 subtasks.

// Package withdraw moves money out of trust: a withdrawal is requested, authorized through
// the trust.withdrawal gate (whose effect re-checks the balance), and posts when a different
// user records its execution.
package withdraw

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned for an unknown withdrawal id.
	ErrNotFound = errors.New("withdraw: not found")
	// ErrBearerPayee is returned for a withdrawal payable to cash or bearer (422).
	ErrBearerPayee = errors.New("withdraw: payee is cash or bearer")
	// ErrInvalid is returned for a request with a non-positive amount or missing fields (422).
	ErrInvalid = errors.New("withdraw: invalid request")
	// ErrUnclearedFunds is returned when the withdrawal draws on uncleared receipts (409 uncleared_funds).
	ErrUnclearedFunds = errors.New("withdraw: uncleared funds")
	// ErrTrustOverdraw is returned when the amount exceeds the available balance (409 trust_overdraw).
	ErrTrustOverdraw = errors.New("withdraw: trust overdraw")
	// ErrNotAuthorized is returned when execution is recorded for a withdrawal that is not
	// authorized and unexpired, or by the authorizer (409 not_authorized).
	ErrNotAuthorized = errors.New("withdraw: not authorized")
	// ErrPreparerApproves is returned when the preparer authorizes their own withdrawal.
	ErrPreparerApproves = errors.New("withdraw: preparer cannot authorize")
	// ErrNoEFTAuthorization is returned when an EFT has no authorization record (Form 9A).
	ErrNoEFTAuthorization = errors.New("withdraw: EFT authorization missing")
)

// Method is how trust money leaves the account.
type Method string

// Methods.
const (
	MethodCheque Method = "cheque"
	MethodEFT    Method = "eft"
)

// Status is a withdrawal's state.
type Status string

// Statuses.
const (
	StatusRequested  Status = "requested"
	StatusAuthorized Status = "authorized"
	StatusExecuted   Status = "executed"
	StatusRejected   Status = "rejected"
)

// Request asks to withdraw AmountCents from one matter's trust ledger.
type Request struct {
	TrustAccountID     string
	MatterID           string
	AmountCents        int64
	Payee              string
	Method             Method
	EFTAuthorizationID string // Form 9A record; required when Method is MethodEFT
	Reason             string
	PreparedBy         string
	IdempotencyKey     string
	// BillID is the delivered bill a fee withdrawal pays (trust.withdrawal_request.bill_id).
	// Additive (WD-BILL): the trust module re-checks it at authorization.
	BillID string
}

// Withdrawal is a request and where it is in its life.
type Withdrawal struct {
	ID           string
	Request      Request
	Status       Status
	GateID       string
	AuthorizedBy string
	AuthorizedAt time.Time
	ExpiresAt    time.Time
	ExecutedBy   string
	ExecutedOn   time.Time // business date in America/Toronto
}

// Service is the withdrawal surface other tasks call.
type Service interface {
	// Request validates r and records it; it opens the trust.withdrawal gate.
	Request(ctx context.Context, r Request) (Withdrawal, error)
	// Authorize is the gate effect: it re-checks the available balance and marks the
	// withdrawal authorized by a user other than the preparer.
	Authorize(ctx context.Context, id, by string, at time.Time) (Withdrawal, error)
	// RecordExecution posts the trust transaction, dated executedOn, by a user other than
	// the authorizer, while the authorization is unexpired.
	RecordExecution(ctx context.Context, id, by string, executedOn time.Time) (Withdrawal, error)
	// Get returns one withdrawal or ErrNotFound.
	Get(ctx context.Context, id string) (Withdrawal, error)
	// Available is cleared posted minus protected minus minimum minus authorized-not-executed.
	Available(ctx context.Context, trustAccountID, matterID string) (int64, error)
}
