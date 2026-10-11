// CONTRACT: app/legal/books (T-010L). Frozen once on main; changes are additive only and need
// a docs/QUESTIONS.md entry. Source: spec/eng/b3-billing-engine.json (chart_of_accounts,
// BIL-I-002, BIL-I-013) and migrations table gl.vendor_bill. Bodies live in the T-010L
// subtasks (chart.go, invariants.go, vendorbill.go).

// Package books owns the firm's operating and trust charts of accounts, the reconciliation
// invariants between the ledger and the billing subledgers, and vendor bills (gl.vendor_bill).
package books

import (
	"context"
	"errors"
	"time"
)

// Book names. Operating and trust are separate books; an entry belongs to exactly one.
const (
	BookOperating = "operating"
	BookTrust     = "trust"
)

// Account codes, from b3 chart_of_accounts. OP-* lives in the operating book, TR-* in trust.
const (
	AcctOperatingBank       = "OP-1000"
	AcctProcessorClearing   = "OP-1010"
	AcctARFees              = "OP-1100"
	AcctARInterest          = "OP-1110"
	AcctWIP                 = "OP-1200"
	AcctDisbRecoverable     = "OP-1210"
	AcctITCRecoverable      = "OP-1220"
	AcctWIPOffset           = "OP-1290"
	AcctSalesTaxPayable     = "OP-2100"
	AcctProvincialTax       = "OP-2110"
	AcctClientCredit        = "OP-2200"
	AcctDueToTrust          = "OP-2300"
	AcctTrustInOperating    = "OP-2400"
	AcctFeeRevenue          = "OP-4000"
	AcctCostRecoveryRevenue = "OP-4010"
	AcctInterestIncome      = "OP-4020"
	AcctSurchargeRevenue    = "OP-4030"
	AcctFeeAdjustments      = "OP-4900"
	AcctBadDebt             = "OP-6100"
	AcctProcessingFees      = "OP-6200"
	AcctTrustBank           = "TR-1500"
	AcctClientTrustLedger   = "TR-2500"
)

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrCrossBook means an entry mixes operating and trust accounts (BIL-I-002).
	ErrCrossBook = errors.New("books: entry mixes operating and trust accounts")
	// ErrUnknownAccount means a line names an account not in the chart.
	ErrUnknownAccount = errors.New("books: unknown account")
	// ErrUnbalanced means an entry's debits and credits differ.
	ErrUnbalanced = errors.New("books: entry does not balance")
	// ErrMismatch means a ledger balance differs from its subledger (BIL-I-013).
	ErrMismatch = errors.New("books: ledger and subledger disagree")
	// ErrNotFound means no vendor bill has the id.
	ErrNotFound = errors.New("books: vendor bill not found")
	// ErrBadStatus means the vendor bill's status does not allow the change.
	ErrBadStatus = errors.New("books: vendor bill status does not allow this")
	// ErrInvalid means a vendor bill fails a field check (amount must be > 0, tax >= 0).
	ErrInvalid = errors.New("books: invalid vendor bill")
)

// Account is one line of a chart.
type Account struct {
	Book string
	Code string
	Name string
	Type string // asset, liability, equity, revenue, expense, contra-asset, contra-revenue
}

// Line is one journal line. Exactly one of DebitCents and CreditCents is non-zero.
type Line struct {
	Account     string
	DebitCents  int64
	CreditCents int64
}

// Entry is a proposed journal entry, checked before the ledger engine posts it.
type Entry struct {
	Book  string
	Lines []Line
}

// Chart is the firm's charts of accounts and the entry checks that depend on them.
type Chart interface {
	// Accounts returns every account of both books, ordered by book then code.
	Accounts() []Account
	// CheckEntry returns ErrCrossBook, ErrUnknownAccount or ErrUnbalanced, wrapped, or nil.
	CheckEntry(e Entry) error
}

// Snapshot is one reconciliation input: ledger balances and the subledger sums they must equal.
type Snapshot struct {
	ARBalanceCents         int64 // OP-1100 + OP-1110
	OpenBillBalancesCents  int64 // sum of open bill balances
	ClientCreditLedger     int64 // OP-2200
	ClientCreditSubledger  int64 // sum of client credit
	WIPLedgerCents         int64 // OP-1200
	UnbilledTimeValueCents int64 // sum of recorded value of approved unbilled time
}

// Mismatch is one failed invariant. It carries account codes and amounts, never client names.
type Mismatch struct {
	Rule        string // rule id, e.g. "BIL-I-013"
	Account     string
	LedgerCents int64
	SubCents    int64
}

// Reconciler checks the ledger against the billing subledgers.
type Reconciler interface {
	// Reconcile returns every mismatch in s. A non-empty result comes with ErrMismatch.
	Reconcile(ctx context.Context, s Snapshot) ([]Mismatch, error)
}

// VendorBillStatus is gl.vendor_bill.status.
type VendorBillStatus string

// Vendor bill statuses.
const (
	VendorBillOpen VendorBillStatus = "open"
	VendorBillPaid VendorBillStatus = "paid"
	VendorBillVoid VendorBillStatus = "void"
)

// VendorBill is one row of gl.vendor_bill. Dates are America/Toronto calendar dates.
type VendorBill struct {
	ID          string
	Vendor      string
	BillDate    time.Time
	DueDate     *time.Time
	AmountCents int64
	TaxCents    int64
	MatterID    string // optional
	DocumentID  string // optional
	Status      VendorBillStatus
}

// VendorBills stores vendor bills. Rows are never deleted; a wrong bill is voided.
type VendorBills interface {
	Create(ctx context.Context, b VendorBill) (VendorBill, error)
	Get(ctx context.Context, id string) (VendorBill, error)
	// MarkPaid moves open -> paid; Void moves open -> void. Other moves return ErrBadStatus.
	MarkPaid(ctx context.Context, id string) (VendorBill, error)
	Void(ctx context.Context, id string) (VendorBill, error)
}
