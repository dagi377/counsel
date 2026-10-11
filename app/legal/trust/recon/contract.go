// CONTRACT: app/legal/trust/recon. The shape of this file is frozen after wave 0; changes
// are additive only and need a docs/QUESTIONS.md entry. Bodies live in the T-013 subtasks.

package recon

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors.
var (
	ErrNotFound = errors.New("recon: not found")
	// ErrDuplicateStatement: a statement for this account and month is already imported.
	ErrDuplicateStatement = errors.New("recon: statement already imported")
	// ErrStatementMismatch: opening plus lines does not equal the closing balance.
	ErrStatementMismatch = errors.New("recon: statement lines do not sum to closing balance")
	// ErrUnbalanced: adjusted bank, book and client-ledger totals differ.
	ErrUnbalanced = errors.New("recon: three-way totals do not agree")
	// ErrSelfApproval: the preparer tried to approve their own reconciliation.
	ErrSelfApproval = errors.New("recon: preparer cannot approve")
	// ErrAgentApproval: an agent principal tried to approve.
	ErrAgentApproval = errors.New("recon: agent cannot approve")
	// ErrPeriodLocked: the month is reconciled and locked; post a dated correction instead.
	ErrPeriodLocked = errors.New("recon: period is reconciled and locked")
	// ErrInvalidState: the reconciliation is not in a state that allows this step.
	ErrInvalidState = errors.New("recon: invalid state")
)

// Period is a calendar month in America/Toronto.
type Period struct {
	Year  int
	Month time.Month
}

// StatementLine is one bank statement line. Amounts are signed int64 cents (deposits > 0).
type StatementLine struct {
	ID          string
	Date        time.Time
	AmountCents int64
	Reference   string
}

// Statement is one month of a trust bank account statement.
type Statement struct {
	ID           string
	AccountID    string
	Period       Period
	OpeningCents int64
	ClosingCents int64
	Lines        []StatementLine
}

// ItemKind classifies a reconciling item.
type ItemKind string

// Reconciling item kinds.
const (
	OutstandingDeposit ItemKind = "outstanding_deposit"
	OutstandingCheque  ItemKind = "outstanding_cheque"
	BankError          ItemKind = "bank_error"
	InTransit          ItemKind = "in_transit"
)

// ReconcilingItem explains a difference between the bank and the book. AmountCents is the
// signed adjustment to the bank balance.
type ReconcilingItem struct {
	Kind        ItemKind
	Reference   string
	AmountCents int64
	Since       time.Time
}

// ThreeWay is the month-end comparison of bank, book and the sum of client ledgers.
type ThreeWay struct {
	BankCents         int64
	AdjustedBankCents int64
	BookCents         int64
	ClientLedgerCents int64
	Items             []ReconcilingItem
}

// Balanced reports whether the adjusted bank balance, the book and the client ledgers agree.
func (t ThreeWay) Balanced() bool {
	return t.AdjustedBankCents == t.BookCents && t.BookCents == t.ClientLedgerCents
}

// Status is a reconciliation's lifecycle state.
type Status string

// Statuses.
const (
	StatusPrepared Status = "prepared"
	StatusApproved Status = "approved"
)

// Approver is who signs off a reconciliation.
type Approver struct {
	UserID  string
	IsAgent bool
}

// Reconciliation is one account's monthly three-way reconciliation.
type Reconciliation struct {
	ID         string
	AccountID  string
	Period     Period
	Result     ThreeWay
	Status     Status
	PreparedBy string
	ApprovedBy string
	ApprovedAt *time.Time
}

// Service imports statements and prepares, approves and locks reconciliations.
type Service interface {
	ImportStatement(ctx context.Context, s Statement) (string, error)
	// Prepare computes the three-way comparison for the month. preparedBy is the user (or
	// the user an agent acts for).
	Prepare(ctx context.Context, accountID string, p Period, items []ReconcilingItem, preparedBy string) (Reconciliation, error)
	// Approve signs off a balanced reconciliation and locks the period.
	Approve(ctx context.Context, id string, by Approver) error
	Get(ctx context.Context, id string) (Reconciliation, error)
	// Locked reports whether the account's month is reconciled and locked.
	Locked(ctx context.Context, accountID string, p Period) (bool, error)
}
