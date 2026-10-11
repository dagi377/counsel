// Package billfake is an in-memory bill.Service for tests: no network, Postgres or Docker.
package billfake

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // America/Toronto without a system zoneinfo

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/jackc/pgx/v5"
)

var _ bill.Service = (*Fake)(nil)

var toronto = mustZone("America/Toronto")

func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// businessDate is t's calendar date in America/Toronto, at midnight there.
func businessDate(t time.Time) time.Time {
	y, m, d := t.In(toronto).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, toronto)
}

// Fake is an in-memory bill service.
type Fake struct {
	mu    sync.Mutex
	now   func() time.Time
	bills map[bill.ID]bill.Bill
	next  map[string]int // "<prefix>-<year>" -> last number used
}

// New returns an empty service that reads the time from now.
func New(now func() time.Time) *Fake {
	return &Fake{now: now, bills: map[bill.ID]bill.Bill{}, next: map[string]int{}}
}

// Put stores a drafted bill, as billing/draft would. Its total must balance.
func (f *Fake) Put(b bill.Bill) error {
	if b.ID == "" || b.TotalCents != b.FeesCents+b.TaxableDisbCents+b.NontaxableDisbCents-b.DiscountCents+b.TaxCents {
		return fmt.Errorf("put: %w", bill.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	b.Lines = slices.Clone(b.Lines)
	if b.Version == 0 {
		b.Version = 1
	}
	f.bills[b.ID] = b
	return nil
}

// List returns bills by id after flt.After, at most flt.Limit (0: all), filtered by matter
// and status.
func (f *Fake) List(_ context.Context, flt bill.Filter) ([]bill.Bill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []bill.Bill{}
	for _, b := range f.bills {
		if (flt.MatterID == "" || b.MatterID == flt.MatterID) && (flt.After == "" || b.ID > flt.After) &&
			(len(flt.Statuses) == 0 || slices.Contains(flt.Statuses, b.Status)) {
			out = append(out, clone(b))
		}
	}
	slices.SortFunc(out, func(a, b bill.Bill) int { return strings.Compare(string(a.ID), string(b.ID)) })
	if flt.Limit > 0 && len(out) > flt.Limit {
		out = out[:flt.Limit]
	}
	return out, nil
}

func clone(b bill.Bill) bill.Bill {
	b.Lines = slices.Clone(b.Lines)
	return b
}

// Get returns one bill.
func (f *Fake) Get(_ context.Context, id bill.ID) (bill.Bill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bills[id]
	if !ok {
		return bill.Bill{}, fmt.Errorf("get %s: %w", id, bill.ErrNotFound)
	}
	return clone(b), nil
}

// Approve assigns the next gapless number and freezes the bill.
func (f *Fake) Approve(_ context.Context, _ pgx.Tx, a bill.Approval) (bill.Bill, error) {
	if a.ApprovalID == "" || a.ApprovedBy == "" || a.Prefix == "" || a.ApprovedAt.IsZero() {
		return bill.Bill{}, fmt.Errorf("approve: %w", bill.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bills[a.BillID]
	if !ok {
		return bill.Bill{}, fmt.Errorf("approve %s: %w", a.BillID, bill.ErrNotFound)
	}
	if b.ApprovalID == a.ApprovalID && b.Status != bill.StatusPendingApproval {
		return clone(b), nil
	}
	if b.Status != bill.StatusPendingApproval {
		return bill.Bill{}, fmt.Errorf("approve %s: %w", a.BillID, bill.ErrNotPending)
	}
	if a.ApprovedBy == b.PreparedBy {
		return bill.Bill{}, fmt.Errorf("approve %s: %w", a.BillID, bill.ErrPreparerApproves)
	}
	issue := businessDate(a.ApprovedAt)
	key := fmt.Sprintf("%s-%04d", a.Prefix, issue.Year())
	f.next[key]++
	b.Number = fmt.Sprintf("%s-%04d", key, f.next[key])
	b.Status = bill.StatusApproved
	b.IssueDate = issue
	b.ApprovalID, b.ApprovedBy, b.ApprovedAt = a.ApprovalID, a.ApprovedBy, a.ApprovedAt
	b.Version++
	f.bills[b.ID] = b
	return clone(b), nil
}

// Update edits a draft bill; approved bills are frozen.
func (f *Fake) Update(_ context.Context, _ pgx.Tx, id bill.ID, c bill.Change) (bill.Bill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bills[id]
	if !ok {
		return bill.Bill{}, fmt.Errorf("update %s: %w", id, bill.ErrNotFound)
	}
	if c.DeliveredAt != nil {
		return bill.Bill{}, fmt.Errorf("update %s: delivered_at: %w", id, bill.ErrReadOnlyField)
	}
	draft := b.Status == bill.StatusDraft || b.Status == bill.StatusPreflightFailed || b.Status == bill.StatusPendingApproval
	if !draft {
		if c.DiscountCents != nil {
			return bill.Bill{}, fmt.Errorf("update %s: %w", id, bill.ErrUseCreditNote)
		}
		return bill.Bill{}, fmt.Errorf("update %s: %w", id, bill.ErrBillGuard)
	}
	if c.DiscountCents != nil {
		d := *c.DiscountCents
		if d < 0 || d > b.FeesCents+b.TaxableDisbCents {
			return bill.Bill{}, fmt.Errorf("update %s: discount: %w", id, bill.ErrInvalid)
		}
		b.TotalCents += b.DiscountCents - d
		b.DiscountCents = d
	}
	if c.DueDate != nil {
		b.DueDate = *c.DueDate
	}
	if c.InterestRatePPM != nil {
		r := *c.InterestRatePPM
		b.InterestRatePPM = &r
	}
	if c.Lines != nil {
		b.Lines = slices.Clone(c.Lines)
	}
	b.Version++
	f.bills[id] = b
	return clone(b), nil
}

// Sign records the licensee signature on an approved bill.
func (f *Fake) Sign(_ context.Context, _ pgx.Tx, id bill.ID, s bill.Signature) error {
	if s.SignedBy == "" || s.Hash == "" || s.SignedAt.IsZero() {
		return fmt.Errorf("sign %s: %w", id, bill.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bills[id]
	if !ok {
		return fmt.Errorf("sign %s: %w", id, bill.ErrNotFound)
	}
	if b.Status != bill.StatusApproved {
		return fmt.Errorf("sign %s: %w", id, bill.ErrNotApproved)
	}
	if b.SignedBy != "" {
		return fmt.Errorf("sign %s: signature: %w", id, bill.ErrBillGuard)
	}
	b.SignedBy, b.SignedAt, b.SignatureHash = s.SignedBy, s.SignedAt, s.Hash
	f.bills[id] = b
	return nil
}

// Deliver records delivery evidence; DeliveredAt goes from zero to a past time once.
func (f *Fake) Deliver(_ context.Context, _ pgx.Tx, id bill.ID, ev bill.DeliveryEvidence) (bill.Bill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bills[id]
	if !ok {
		return bill.Bill{}, fmt.Errorf("deliver %s: %w", id, bill.ErrNotFound)
	}
	switch {
	case !b.DeliveredAt.IsZero():
		return bill.Bill{}, fmt.Errorf("deliver %s: %w", id, bill.ErrAlreadyDelivered)
	case b.Status != bill.StatusApproved:
		return bill.Bill{}, fmt.Errorf("deliver %s: %w", id, bill.ErrNotApproved)
	case b.SignedBy == "":
		return bill.Bill{}, fmt.Errorf("deliver %s: %w", id, bill.ErrSignatureRequired)
	case ev.AcceptedAt.IsZero():
		return bill.Bill{}, fmt.Errorf("deliver %s: %w", id, bill.ErrInvalid)
	case ev.AcceptedAt.After(f.now()):
		return bill.Bill{}, fmt.Errorf("deliver %s: %w", id, bill.ErrFutureEvidence)
	}
	if ev.Bounced {
		return clone(b), nil
	}
	b.DeliveredAt = ev.AcceptedAt
	b.DeliveredOn = businessDate(ev.AcceptedAt)
	b.DeliveryChannel = ev.Channel
	b.Status = bill.StatusDelivered
	b.Version++
	f.bills[id] = b
	return clone(b), nil
}
