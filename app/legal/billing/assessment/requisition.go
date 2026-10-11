package assessment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/interest"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/flags"
)

// Requisition errors.
var (
	// ErrSpecialCircumstances means the requisition is past the 12-month window and has no
	// special-circumstances note (SPECIAL_CIRCUMSTANCES_REQUIRED).
	ErrSpecialCircumstances = errors.New("assessment: special-circumstances note required")
	// ErrNotDelivered means the bill has not been delivered, so it cannot be assessed yet.
	ErrNotDelivered = errors.New("assessment: bill not delivered")
	// ErrRequisitionUnverified means the Solicitors Act flag is off.
	ErrRequisitionUnverified = errors.New("assessment: requisitions disabled until Solicitors Act rules are verified")
)

// Requisition statuses. A window never shows "expired" (BIL-L-005). Within one month of
// delivery assessment is available as of right; from then to 12 months the court may refer
// the bill without special circumstances; after 12 months special circumstances are needed
// (Solicitors Act s.3, s.4).
const (
	StatusWindowOpen           = "requisition window open"
	StatusCourtReferral        = "court referral available"
	StatusSpecialCircumstances = "special circumstances required"
)

// AssessedBill is what a requisition needs to know about one bill.
type AssessedBill struct {
	ID          string
	DeliveredAt time.Time // zero when not delivered
	Paid        bool      // payment never closes the assessment option
}

// Requisition is one recorded assessment requisition.
type Requisition struct {
	BillID               string
	RecordedOn           time.Time // business date
	SpecialCircumstances string    // required after the 12-month window
}

// Windows are the per-bill assessment dates shown on the bill page.
type Windows struct {
	ClientRequisitionTo time.Time // months_after(delivery date, 1)
	TwelveMonthTo       time.Time // months_after(delivery date, 12)
	Status              string
}

// Requisitions records assessment requisitions and answers whether a bill is on hold.
type Requisitions struct {
	Calendar interest.Calendar // the one shared months_after
	Clock    clock.Clock
	Flags    flags.Flags

	mu     sync.Mutex
	byBill map[string]Requisition
}

// Windows returns the assessment windows of b as of today.
func (r *Requisitions) Windows(ctx context.Context, b AssessedBill) (Windows, error) {
	if b.DeliveredAt.IsZero() {
		return Windows{}, ErrNotDelivered
	}
	loc := r.Clock.Location()
	y, m, d := b.DeliveredAt.In(loc).Date()
	delivered := time.Date(y, m, d, 0, 0, 0, 0, loc)
	// rule: BIL-L-005
	w := Windows{
		ClientRequisitionTo: r.Calendar.MonthsAfter(delivered, 1),
		TwelveMonthTo:       r.Calendar.MonthsAfter(delivered, 12),
		Status:              StatusWindowOpen,
	}
	// rule: BIL-L-005 (verify)
	switch today := r.today(ctx); {
	case today.After(w.TwelveMonthTo):
		w.Status = StatusSpecialCircumstances
	case today.After(w.ClientRequisitionTo):
		w.Status = StatusCourtReferral
	}
	return w, nil
}

// Record records a requisition on b today. After the 12-month window it needs a
// special-circumstances note. A paid bill can still be assessed.
func (r *Requisitions) Record(ctx context.Context, b AssessedBill, note string) (Requisition, error) {
	if !r.Flags.Enabled(FlagSolicitorsActVerified) {
		return Requisition{}, ErrRequisitionUnverified
	}
	w, err := r.Windows(ctx, b)
	if err != nil {
		return Requisition{}, fmt.Errorf("bill %s: %w", b.ID, err)
	}
	note = strings.TrimSpace(note)
	// rule: BIL-L-005 (verify)
	if w.Status == StatusSpecialCircumstances && note == "" {
		return Requisition{}, fmt.Errorf("bill %s: %w", b.ID, ErrSpecialCircumstances)
	}
	// rule: BIL-L-006
	// b.Paid is deliberately not checked: payment does not close the assessment option.
	req := Requisition{BillID: b.ID, RecordedOn: r.today(ctx), SpecialCircumstances: note}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byBill == nil {
		r.byBill = map[string]Requisition{}
	}
	r.byBill[b.ID] = req
	return req, nil
}

// OnHold reports whether billID has a recorded requisition. While it does, the bill gets no
// reminders and no interest posting.
func (r *Requisitions) OnHold(billID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.byBill[billID]
	return ok
}

// RemindersAllowed reports whether payment reminders may go out on billID.
func (r *Requisitions) RemindersAllowed(billID string) bool {
	// rule: BIL-L-005
	return !r.OnHold(billID)
}

// InterestAllowed reports whether interest may be posted on billID.
func (r *Requisitions) InterestAllowed(billID string) bool {
	// rule: BIL-L-005
	return !r.OnHold(billID)
}

func (r *Requisitions) today(ctx context.Context) time.Time {
	t := r.Clock.Today(ctx)
	return time.Date(t.Year, t.Month, t.Day, 0, 0, 0, 0, r.Clock.Location())
}
