// Package creditfake is an in-memory credit.Service and credit.Bills for tests. No network,
// Postgres or Docker.
package creditfake

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/credit"
)

var (
	_ credit.Service = (*Fake)(nil)
	_ credit.Bills   = (*Fake)(nil)
)

// Fake is a concurrency-safe in-memory credit store with its own bills.
type Fake struct {
	// Now returns the current time; nil means time.Now.
	Now func() time.Time

	mu        sync.Mutex
	seq       int
	noteSeq   int
	bills     map[string]credit.Bill
	notes     map[string]*credit.Note
	writeOffs map[string]*credit.WriteOff
}

// New returns an empty Fake.
func New() *Fake {
	return &Fake{bills: map[string]credit.Bill{}, notes: map[string]*credit.Note{}, writeOffs: map[string]*credit.WriteOff{}}
}

// PutBill adds or replaces a bill.
func (f *Fake) PutBill(b credit.Bill) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bills[b.ID] = b
}

func (f *Fake) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// Bill implements credit.Bills.
func (f *Fake) Bill(ctx context.Context, id string) (credit.Bill, error) {
	if err := ctx.Err(); err != nil {
		return credit.Bill{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bill(id)
}

func (f *Fake) bill(id string) (credit.Bill, error) {
	b, ok := f.bills[id]
	if !ok {
		return credit.Bill{}, fmt.Errorf("bill %s: %w", id, credit.ErrNotFound)
	}
	return b, nil
}

func (f *Fake) checkAmount(billID string, cents int64) error {
	b, err := f.bill(billID)
	if err != nil {
		return err
	}
	if !b.Delivered {
		return credit.ErrBillNotDelivered
	}
	if cents <= 0 {
		return credit.ErrInvalidAmount
	}
	if cents > b.OpenCents {
		return credit.ErrExceedsBalance
	}
	return nil
}

func checkApprover(preparedBy string, by credit.Approver) error {
	if by.IsAgent {
		return credit.ErrAgentApproval
	}
	if by.UserID == "" || by.UserID == preparedBy {
		return credit.ErrSelfApproval
	}
	return nil
}

// DraftNote implements credit.Service.
func (f *Fake) DraftNote(ctx context.Context, n credit.Note) (credit.Note, error) {
	if err := ctx.Err(); err != nil {
		return credit.Note{}, err
	}
	for _, l := range n.Lines {
		if l.FeeCents < 0 || l.TaxCents < 0 {
			return credit.Note{}, fmt.Errorf("draft note: %w", credit.ErrInvalidAmount)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.checkAmount(n.BillID, n.TotalCents()); err != nil {
		return credit.Note{}, fmt.Errorf("draft note: %w", err)
	}
	f.seq++
	n.ID, n.Number, n.Status = "cn-"+strconv.Itoa(f.seq), "", credit.StatusDraft
	n.ApprovedBy, n.ApprovedAt = "", nil
	n.Lines = append([]credit.Line(nil), n.Lines...)
	f.notes[n.ID] = &n
	return n, nil
}

// ApproveNote implements credit.Service.
func (f *Fake) ApproveNote(ctx context.Context, id string, by credit.Approver) (credit.Note, error) {
	if err := ctx.Err(); err != nil {
		return credit.Note{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.notes[id]
	if !ok {
		return credit.Note{}, fmt.Errorf("approve note %s: %w", id, credit.ErrNotFound)
	}
	if err := checkApprover(n.PreparedBy, by); err != nil {
		return credit.Note{}, fmt.Errorf("approve note %s: %w", id, err)
	}
	if n.Status != credit.StatusDraft {
		return credit.Note{}, fmt.Errorf("approve note %s: %w", id, credit.ErrInvalidState)
	}
	// Re-check: the balance may have moved since the draft.
	if err := f.checkAmount(n.BillID, n.TotalCents()); err != nil {
		return credit.Note{}, fmt.Errorf("approve note %s: %w", id, err)
	}
	f.noteSeq++
	t := f.now()
	n.Number, n.Status, n.ApprovedBy, n.ApprovedAt = fmt.Sprintf("CN-%04d", f.noteSeq), credit.StatusApproved, by.UserID, &t
	b := f.bills[n.BillID]
	b.OpenCents -= n.TotalCents()
	f.bills[n.BillID] = b
	return *n, nil
}

// Note implements credit.Service.
func (f *Fake) Note(ctx context.Context, id string) (credit.Note, error) {
	if err := ctx.Err(); err != nil {
		return credit.Note{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.notes[id]
	if !ok {
		return credit.Note{}, fmt.Errorf("note %s: %w", id, credit.ErrNotFound)
	}
	return *n, nil
}

// DraftWriteOff implements credit.Service.
func (f *Fake) DraftWriteOff(ctx context.Context, w credit.WriteOff) (credit.WriteOff, error) {
	if err := ctx.Err(); err != nil {
		return credit.WriteOff{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.checkAmount(w.BillID, w.AmountCents); err != nil {
		return credit.WriteOff{}, fmt.Errorf("draft write-off: %w", err)
	}
	f.seq++
	w.ID, w.Status, w.ApprovedBy, w.ApprovedAt = "wo-"+strconv.Itoa(f.seq), credit.StatusDraft, "", nil
	f.writeOffs[w.ID] = &w
	return w, nil
}

// ApproveWriteOff implements credit.Service.
func (f *Fake) ApproveWriteOff(ctx context.Context, id string, by credit.Approver) (credit.WriteOff, error) {
	if err := ctx.Err(); err != nil {
		return credit.WriteOff{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.writeOffs[id]
	if !ok {
		return credit.WriteOff{}, fmt.Errorf("approve write-off %s: %w", id, credit.ErrNotFound)
	}
	if err := checkApprover(w.PreparedBy, by); err != nil {
		return credit.WriteOff{}, fmt.Errorf("approve write-off %s: %w", id, err)
	}
	if w.Status != credit.StatusDraft {
		return credit.WriteOff{}, fmt.Errorf("approve write-off %s: %w", id, credit.ErrInvalidState)
	}
	if err := f.checkAmount(w.BillID, w.AmountCents); err != nil {
		return credit.WriteOff{}, fmt.Errorf("approve write-off %s: %w", id, err)
	}
	t := f.now()
	w.Status, w.ApprovedBy, w.ApprovedAt = credit.StatusApproved, by.UserID, &t
	b := f.bills[w.BillID]
	b.OpenCents -= w.AmountCents
	f.bills[w.BillID] = b
	return *w, nil
}

// WriteOff implements credit.Service.
func (f *Fake) WriteOff(ctx context.Context, id string) (credit.WriteOff, error) {
	if err := ctx.Err(); err != nil {
		return credit.WriteOff{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.writeOffs[id]
	if !ok {
		return credit.WriteOff{}, fmt.Errorf("write-off %s: %w", id, credit.ErrNotFound)
	}
	return *w, nil
}
