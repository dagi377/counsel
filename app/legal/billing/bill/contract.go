// CONTRACT: app/legal/billing/bill. Frozen once on main; changes are additive only and
// need a docs/QUESTIONS.md entry. Bodies live in the T-019 subtasks (approve.go, guard.go,
// deliver.go, pdf.go, journal.go, paralegal.go).
// Source: migrations/0001_init.sql (billing.bill, billing.bill_line), spec/eng/b3.

// Package bill approves, numbers, signs and delivers bills. A bill is drafted elsewhere
// (billing/draft); from approval on its amounts, number and terms are frozen.
package bill

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ID identifies a bill.
type ID string

// Status is the state of a bill.
type Status string

// Bill states. Only the bill approval gate effect moves a bill to StatusApproved.
const (
	StatusDraft           Status = "draft"
	StatusPreflightFailed Status = "preflight_failed"
	StatusPendingApproval Status = "pending_approval"
	StatusApproved        Status = "approved"
	StatusDelivered       Status = "delivered"
	StatusPartiallyPaid   Status = "partially_paid"
	StatusPaid            Status = "paid"
	StatusWrittenOff      Status = "written_off"
	StatusCredited        Status = "credited"
	StatusVoid            Status = "void"
)

// SourceType is where a bill line comes from.
type SourceType string

// Line sources. Only an adjustment may be negative.
const (
	SourceTime       SourceType = "time"
	SourceExpense    SourceType = "expense"
	SourceFlat       SourceType = "flat"
	SourceAdjustment SourceType = "adjustment"
)

// Channel is how a bill was delivered.
type Channel string

// Delivery channels.
const (
	ChannelEmail  Channel = "email"
	ChannelPortal Channel = "portal"
	ChannelMail   Channel = "mail"
	ChannelHand   Channel = "hand"
)

// Line is one bill line. Amounts are int64 cents.
type Line struct {
	Ordinal       int
	Source        SourceType
	SourceID      string // time entry or expense id
	Narrative     string
	QuantityMilli int64 // hours*1000 for time, units*1000 otherwise
	RateCents     int64
	AmountCents   int64
	TaxCode       string
	FeeBasis      string // the disclosed fee basis this line is charged under
}

// Bill is a bill and its frozen terms. All amounts are int64 cents; rates are ppm.
type Bill struct {
	ID                    ID
	Number                string // prefix-YYYY-NNNN, assigned at approval
	MatterID              string
	PayerID               string
	Status                Status
	PreparedBy            string
	IssueDate             time.Time // approval date in America/Toronto
	DueDate               time.Time
	Lines                 []Line
	FeesCents             int64
	TaxableDisbCents      int64
	NontaxableDisbCents   int64
	DiscountCents         int64
	TaxCents              int64
	TotalCents            int64 // fees + disbursements - discount + tax
	InterestRatePPM       *int64
	PlaceOfSupplyProvince string
	ApprovalID            string
	ApprovedBy            string
	ApprovedAt            time.Time
	SignedBy              string
	SignedAt              time.Time
	SignatureHash         string
	DeliveredAt           time.Time // zero until delivery evidence arrives
	DeliveredOn           time.Time // business date of DeliveredAt, America/Toronto
	DeliveryChannel       Channel
	Paralegal             bool // prepared under a paralegal licence
}

// Approval is what the bill approval gate effect passes in.
type Approval struct {
	BillID     ID
	ApprovalID string // gate id; replaying the same approval is a no-op
	ApprovedBy string
	ApprovedAt time.Time
	Prefix     string // number series prefix
}

// Signature is the licensee's signature on a bill or its cover letter.
type Signature struct {
	SignedBy string
	SignedAt time.Time
	Hash     string // "sha256:" + hex of the signed content
}

// DeliveryEvidence is proof from a channel that a bill reached the payer.
type DeliveryEvidence struct {
	Channel    Channel
	AcceptedAt time.Time // when the channel accepted the bill; never in the future
	Bounced    bool      // a bounce leaves the bill undelivered
}

// Change is a requested edit to a bill. Empty fields are not changed.
type Change struct {
	DiscountCents   *int64
	DueDate         *time.Time
	InterestRatePPM *int64
	DeliveredAt     *time.Time
	Lines           []Line
}

// Service is the bill surface other modules call.
type Service interface {
	// Get returns one bill.
	Get(ctx context.Context, id ID) (Bill, error)
	// Approve is called only by the bill approval gate effect. It assigns the next gapless
	// number for prefix and year in the same tx, sets the issue date and freezes the bill.
	// Replaying an approval already applied returns the approved bill unchanged.
	Approve(ctx context.Context, tx pgx.Tx, a Approval) (Bill, error)
	// Update edits a draft. On an approved bill a discount returns ErrUseCreditNote and any
	// other frozen field returns ErrBillGuard; DeliveredAt is never set this way.
	Update(ctx context.Context, tx pgx.Tx, id ID, c Change) (Bill, error)
	// Sign records the licensee's signature on an approved bill.
	Sign(ctx context.Context, tx pgx.Tx, id ID, s Signature) error
	// Deliver records delivery evidence. It needs a signature and sets DeliveredAt once.
	Deliver(ctx context.Context, tx pgx.Tx, id ID, ev DeliveryEvidence) (Bill, error)
}

// Sentinel errors.
var (
	ErrNotFound          = errors.New("bill: not found")
	ErrInvalid           = errors.New("bill: invalid input")
	ErrNotPending        = errors.New("bill: not pending approval")
	ErrPreparerApproves  = errors.New("bill: preparer may not approve")
	ErrBillGuard         = errors.New("bill: approved bill is frozen")
	ErrUseCreditNote     = errors.New("bill: use a credit note")
	ErrReadOnlyField     = errors.New("bill: read-only field")
	ErrSignatureRequired = errors.New("bill: signature required")
	ErrNotApproved       = errors.New("bill: not approved")
	ErrAlreadyDelivered  = errors.New("bill: already delivered")
	ErrFutureEvidence    = errors.New("bill: delivery evidence in the future")
)
