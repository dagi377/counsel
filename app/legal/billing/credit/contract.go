// CONTRACT: app/legal/billing/credit. The shape of this file is frozen after wave 0; changes
// are additive only and need a docs/QUESTIONS.md entry. Bodies live in the T-023 subtasks.

package credit

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors.
var (
	ErrNotFound = errors.New("credit: not found")
	// ErrBillNotDelivered: before delivery a bill is voided or edited, not credited.
	ErrBillNotDelivered = errors.New("credit: bill is not delivered")
	// ErrInvalidAmount: a zero or negative amount, or a line with negative fee or tax.
	ErrInvalidAmount = errors.New("credit: invalid amount")
	// ErrExceedsBalance: the credit or write-off is larger than the bill's open balance.
	ErrExceedsBalance = errors.New("credit: amount exceeds open balance")
	// ErrSelfApproval: the preparer tried to approve their own credit or write-off.
	ErrSelfApproval = errors.New("credit: preparer cannot approve")
	// ErrAgentApproval: an agent principal tried to approve.
	ErrAgentApproval = errors.New("credit: agent cannot approve")
	// ErrInvalidState: the record is not in a state that allows this step.
	ErrInvalidState = errors.New("credit: invalid state")
)

// Status is the lifecycle state of a credit note or write-off.
type Status string

// Statuses.
const (
	StatusDraft    Status = "draft"
	StatusApproved Status = "approved"
	// StatusRejected: a write-off draft whose G-B5 gate was rejected. Final.
	StatusRejected Status = "rejected"
	// StatusWithdrawn: a write-off draft its preparer took back. Final.
	StatusWithdrawn Status = "withdrawn"
)

// Line credits part of one bill line. Amounts are int64 cents; TaxCents is the sales tax
// reversed with the fee, prorated from the source bill line.
type Line struct {
	BillLineID string
	FeeCents   int64
	TaxCents   int64
}

// Note is a numbered credit note that reduces a delivered bill.
type Note struct {
	ID         string
	Number     string // assigned on approval
	BillID     string
	Lines      []Line
	Reason     string
	Status     Status
	PreparedBy string
	ApprovedBy string
	ApprovedAt *time.Time
}

// TotalCents is the fee plus tax credited by the note.
func (n Note) TotalCents() int64 {
	var t int64
	for _, l := range n.Lines {
		t += l.FeeCents + l.TaxCents
	}
	return t
}

// WriteOff removes an uncollectible open balance from a delivered bill.
type WriteOff struct {
	ID          string
	BillID      string
	AmountCents int64
	Reason      string
	Status      Status
	PreparedBy  string
	ApprovedBy  string
	ApprovedAt  *time.Time
	// InterestCents is the part of AmountCents that is posted interest (the draft's interest
	// bucket). Additive (PORT-16b): the bad-debt entry credits it to OP-1110.
	InterestCents int64
}

// Approver is who approves a credit note or write-off.
type Approver struct {
	UserID  string
	IsAgent bool
}

// Bill is the slice of a bill this module reads.
type Bill struct {
	ID              string
	Delivered       bool
	OpenCents       int64 // unpaid balance, fee plus tax
	ResponsibleUser string
}

// Bills reads bills owned by the billing module.
type Bills interface {
	Bill(ctx context.Context, id string) (Bill, error)
}

// Service drafts and approves credit notes and write-offs. Approval reduces the bill's open
// balance; nothing is deleted.
type Service interface {
	DraftNote(ctx context.Context, n Note) (Note, error)
	ApproveNote(ctx context.Context, id string, by Approver) (Note, error)
	Note(ctx context.Context, id string) (Note, error)
	DraftWriteOff(ctx context.Context, w WriteOff) (WriteOff, error)
	ApproveWriteOff(ctx context.Context, id string, by Approver) (WriteOff, error)
	WriteOff(ctx context.Context, id string) (WriteOff, error)
}
