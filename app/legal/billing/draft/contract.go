// CONTRACT: app/legal/billing/draft (T-017). Frozen once on main; changes are additive only
// and need a docs/QUESTIONS.md entry. Source: spec/eng/b3-billing-engine.json (BIL-C-020..023,
// BIL-C-030, BIL-C-040/041, BIL-L-002), spec/eng/b2-sales-tax.json (BIL-T-033). Bodies live
// in the T-017 subtasks (lines.go, discount.go, disbursement.go, reduction.go, itemize.go).

// Package draft computes the lines of a draft bill: reductions, discounts, disbursements and
// markup, and whether the draft may be delivered. Money is int64 cents; rates are int64 ppm.
package draft

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors. Each maps to problem+json 422 unless noted.
var (
	// ErrNegativeLine means a reduction or discount would make a line negative (BIL-C-021).
	ErrNegativeLine = errors.New("draft: line would go negative")
	// ErrMarkupOnAgent means markup was asked on an agent disbursement (BIL-C-041).
	ErrMarkupOnAgent = errors.New("draft: markup on agent disbursement")
	// ErrReasonRequired means a write-down has no reason, or "other" without text (BIL-C-023).
	ErrReasonRequired = errors.New("draft: write-down reason required")
	// ErrDeliveryBlocked means the draft may not be delivered (BIL-L-002).
	ErrDeliveryBlocked = errors.New("draft: delivery blocked")
	// ErrInvalidDiscount means a discount has a bad kind, base or value.
	ErrInvalidDiscount = errors.New("draft: invalid discount")
	// ErrNotFound means no draft or line has the id (404).
	ErrNotFound = errors.New("draft: not found")
	// ErrNotDraft means the bill left draft state and its lines are frozen (409).
	ErrNotDraft = errors.New("draft: bill is not a draft")
	// ErrInvalidReduction means a reduction is negative. A write-up is its own explicit action,
	// never a negative reduction (additive, docs/QUESTIONS.md).
	ErrInvalidReduction = errors.New("draft: reduction must not be negative")
)

// LineKind is what a bill line bills for.
type LineKind string

// Line kinds. Own disbursements are the firm's costs (HST on rebill, markup allowed); agent
// disbursements are paid as the client's agent (no HST, no markup, no bill discount).
const (
	LineFee      LineKind = "fee"
	LineFlat     LineKind = "flat"
	LineOwnCost  LineKind = "own_cost"
	LineAgentDis LineKind = "agent_disbursement"
)

// Reason is a write-down reason code (BIL-C-023).
type Reason string

// Write-down reasons.
const (
	ReasonDuplicate    Reason = "duplicate"
	ReasonInefficiency Reason = "inefficiency"
	ReasonTraining     Reason = "training"
	ReasonCourtesy     Reason = "courtesy"
	ReasonOverEstimate Reason = "over_estimate"
	ReasonScope        Reason = "scope"
	ReasonOther        Reason = "other"
)

// Reduction is a write-down on one line. It lowers the line only, never the source time entry.
type Reduction struct {
	Cents      int64
	Reason     Reason
	ReasonText string // required when Reason is ReasonOther
	ShowOnBill bool
}

// Line is one draft bill line. Net = AmountCents - Reduction - LineDiscountCents -
// DiscountShareCents; tax is computed on Net.
type Line struct {
	ID                 string
	Kind               LineKind
	SortOrder          int
	Description        string
	ItemDate           *time.Time // disbursements: the date the cost was incurred
	ItemCount          int        // disbursements: >1 means aggregated; needs Items to deliver
	Items              []Item     // per-item detail of an aggregated disbursement
	AmountCents        int64
	MarkupCents        int64
	Reduction          *Reduction
	LineDiscountCents  int64
	DiscountShareCents int64 // this line's share of the bill discount (BIL-C-022)
	Taxable            bool
	SourceTimeEntryID  string
}

// Item is one itemized disbursement inside an aggregated line (BIL-L-002).
type Item struct {
	Date        time.Time
	Description string
	AmountCents int64
}

// DiscountKind says how a bill discount's amount is set.
type DiscountKind string

// Discount kinds.
const (
	DiscountPercent DiscountKind = "percent"
	DiscountFixed   DiscountKind = "fixed"
)

// DiscountBase says which lines a bill discount applies to. Agent disbursements never.
type DiscountBase string

// Discount bases.
const (
	BaseFeesAndOwn DiscountBase = "fees_and_own"
	BaseFeesOnly   DiscountBase = "fees_only"
	BaseOneLine    DiscountBase = "one_line"
)

// Discount is a bill-level discount.
type Discount struct {
	Kind        DiscountKind
	PPM         int64 // percent kind: 100000 = 10%
	AmountCents int64 // fixed kind
	Base        DiscountBase
	LineID      string // BaseOneLine only
}

// Calc is the pure draft arithmetic. It never mutates its input slices.
type Calc interface {
	// Net returns a line's net amount after reductions and discounts.
	Net(l Line) int64
	// ApplyReduction applies r to l (BIL-C-023); returns ErrReasonRequired or ErrNegativeLine.
	ApplyReduction(l Line, r Reduction) (Line, error)
	// ApplyDiscount computes the bill discount (BIL-C-021) and spreads it over eligible lines
	// by largest remainder, ties to the earliest SortOrder (BIL-C-022).
	ApplyDiscount(lines []Line, d Discount) ([]Line, error)
	// Markup returns rhu(cost * ppm, 1e6) for an own cost; ErrMarkupOnAgent for an agent one.
	Markup(kind LineKind, costCents, ppm int64) (int64, error)
	// CheckDelivery returns ErrDeliveryBlocked when an aggregated disbursement lacks per-item
	// date and description (BIL-L-002).
	CheckDelivery(lines []Line) error
}

// Store keeps draft bill lines. Only drafts change; a bill past draft returns ErrNotDraft.
type Store interface {
	Lines(ctx context.Context, billID string) ([]Line, error)
	SaveLines(ctx context.Context, billID string, lines []Line) error
}
