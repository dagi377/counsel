// CONTRACT: app/legal/trust. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-011 subtasks (account.go, receipt.go,
// cashlimit.go, nsf.go, ledgerlock.go).
// Source: migrations/0001_init.sql (trust schema), spec/legal/trust.json, spec/eng/b1.

// Package trust keeps trust accounts, signatories, client ledgers and trust transactions.
// Amounts are int64 cents; a positive amount goes into trust, a negative one comes out.
package trust

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// AccountID identifies a trust bank account.
type AccountID string

// LedgerID identifies one client ledger (one matter on one trust account).
type LedgerID string

// TxnID identifies one trust transaction.
type TxnID string

// AccountType is the kind of trust bank account.
type AccountType string

// Account types.
const (
	AccountMixed                   AccountType = "mixed"
	AccountSeparateInterestBearing AccountType = "separate_interest_bearing"
)

// TxnKind is the kind of a trust transaction.
type TxnKind string

// Transaction kinds. Receipt, opening balance, transfer in and interest are positive;
// disbursement and transfer out are negative; a reversal has the opposite sign of what
// it reverses.
const (
	KindOpeningBalance TxnKind = "opening_balance"
	KindReceipt        TxnKind = "receipt"
	KindDisbursement   TxnKind = "disbursement"
	KindTransferIn     TxnKind = "transfer_in"
	KindTransferOut    TxnKind = "transfer_out"
	KindReversal       TxnKind = "reversal"
	KindInterest       TxnKind = "interest"
)

// Method is how money moved.
type Method string

// Methods. Only cheque and bank draft receipts may start uncleared; cash is never paid out.
const (
	MethodCheque    Method = "cheque"
	MethodEFT       Method = "eft"
	MethodWire      Method = "wire"
	MethodCash      Method = "cash"
	MethodBankDraft Method = "bank_draft"
	MethodCard      Method = "card"
	MethodInternal  Method = "internal"
)

// Account is a trust bank account.
type Account struct {
	ID           AccountID
	Type         AccountType
	Institution  string
	AccountLast4 string
	Title        string
	ForMatterID  string // separate accounts only
	OpenedOn     time.Time
	SignatoryIDs []string // user ids of active signatories
}

// Ledger is one client ledger: the trust money held for one matter on one account.
type Ledger struct {
	ID                  LedgerID
	AccountID           AccountID
	MatterID            string
	ClientID            string
	ProtectedCents      int64
	ProtectedPurpose    string // required when ProtectedCents > 0
	MinimumBalanceCents int64
}

// Txn is one posted trust transaction.
type Txn struct {
	ID                TxnID
	AccountID         AccountID
	LedgerID          LedgerID
	Kind              TxnKind
	TxnDate           time.Time // business date, America/Toronto
	Method            Method
	InstrumentNo      string
	Counterparty      string
	AmountCents       int64
	Purpose           string
	ReversesID        TxnID
	ReplenishesID     TxnID
	WithdrawalID      string // disbursements only
	ApprovalID        string // gate that authorised the posting, when one did
	CashExceptionCode string
	ClearedOn         time.Time // zero while an uncleared cheque or draft
	PostedBy          string
}

// Receipt is money received into trust for one ledger.
type Receipt struct {
	LedgerID          LedgerID
	TxnDate           time.Time
	Method            Method
	InstrumentNo      string
	Counterparty      string
	AmountCents       int64 // > 0
	Purpose           string
	ClearedOn         time.Time // zero only for cheque or bank draft
	CashExceptionCode string    // lets a cash receipt over the limit through, with evidence
	PostedBy          string
}

// Disbursement is money paid out of one ledger for an approved withdrawal. It is called
// only from a gate effect handler.
type Disbursement struct {
	LedgerID     LedgerID
	WithdrawalID string
	ApprovalID   string
	TxnDate      time.Time
	Method       Method // never cash
	Counterparty string
	AmountCents  int64 // > 0; posted as a negative amount
	Purpose      string
	PostedBy     string
}

// Balance is a ledger's balance. Available = Cleared - Protected - Minimum.
type Balance struct {
	LedgerID       LedgerID
	PostedCents    int64
	ClearedCents   int64
	ProtectedCents int64
	MinimumCents   int64
	AvailableCents int64
}

// CashCheck is the result of checking cash received on one client file.
type CashCheck struct {
	TotalCents int64 // cash already received on the file plus the new amount
	Warn       bool  // at or over the warning threshold
	Blocked    bool  // at or over the limit and no exception code
}

// NSFResult is what an NSF reversal posted.
type NSFResult struct {
	ReversalID      TxnID
	ReplenishmentID TxnID // empty when the ledger still covered the reversal
	ShortfallCents  int64
	ExceptionOpened bool
}

// CashSource reports cash taken on a client file outside trust (operating payments), so the
// cash limit counts both.
type CashSource interface {
	OperatingCashCents(ctx context.Context, matterID string) (int64, error)
}

// Accounts manages trust accounts and their signatories.
type Accounts interface {
	OpenAccount(ctx context.Context, tx pgx.Tx, a Account) (AccountID, error)
	AddSignatory(ctx context.Context, tx pgx.Tx, id AccountID, userID string) error
	RemoveSignatory(ctx context.Context, tx pgx.Tx, id AccountID, userID string) error
	Account(ctx context.Context, id AccountID) (Account, error)
}

// Ledgers manages client ledgers and posts trust transactions.
type Ledgers interface {
	// OpenLedger opens the ledger for one matter on one account.
	OpenLedger(ctx context.Context, tx pgx.Tx, l Ledger) (LedgerID, error)
	// Receive posts a receipt. The account needs an active signatory; cash over the limit
	// needs an exception code.
	Receive(ctx context.Context, tx pgx.Tx, r Receipt) (TxnID, error)
	// CheckCash reports how cash of amountCents would stand against the cash limit.
	CheckCash(ctx context.Context, matterID string, amountCents int64) (CashCheck, error)
	// ReverseNSF reverses a receipt returned NSF. When the money was already paid out, a
	// replenishing receipt for exactly the shortfall is posted in the same tx under
	// approvalID and a trust exception is opened.
	ReverseNSF(ctx context.Context, tx pgx.Tx, receipt TxnID, approvalID, by string) (NSFResult, error)
	// Disburse locks the ledger (SELECT FOR UPDATE), re-checks the available balance and
	// posts the disbursement, or returns ErrInsufficientFunds.
	Disburse(ctx context.Context, tx pgx.Tx, d Disbursement) (TxnID, error)
	// Balance returns a ledger's current balance.
	Balance(ctx context.Context, id LedgerID) (Balance, error)
	// Txns lists a ledger's transactions in posting order.
	Txns(ctx context.Context, id LedgerID) ([]Txn, error)
}

// Service is the whole trust surface.
type Service interface {
	Accounts
	Ledgers
}

// Sentinel errors.
var (
	ErrNotFound          = errors.New("trust: not found")
	ErrInvalid           = errors.New("trust: invalid input")
	ErrNoSignatory       = errors.New("trust: account has no active signatory")
	ErrDuplicateLedger   = errors.New("trust: ledger already open for matter on account")
	ErrCashLimit         = errors.New("trust: cash limit reached")
	ErrCashPayout        = errors.New("trust: cash payout not allowed")
	ErrInsufficientFunds = errors.New("trust: insufficient trust funds")
	ErrUnclearedFunds    = errors.New("trust: uncleared funds")
	ErrNotReceipt        = errors.New("trust: transaction is not a receipt")
	ErrAlreadyReversed   = errors.New("trust: already reversed")
)
