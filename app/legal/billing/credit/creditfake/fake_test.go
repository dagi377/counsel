package creditfake

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/billing/credit"
)

func newFake() *Fake {
	f := New()
	f.PutBill(credit.Bill{ID: "b1", Delivered: true, OpenCents: 113000})
	f.PutBill(credit.Bill{ID: "draft", Delivered: false, OpenCents: 113000})
	return f
}

func TestCreditNote(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		billID     string
		lines      []credit.Line
		by         credit.Approver
		draftErr   error
		approveErr error
		wantOpen   int64
	}{
		{name: "partial credit", billID: "b1", lines: []credit.Line{{BillLineID: "l1", FeeCents: 10000, TaxCents: 1300}}, by: credit.Approver{UserID: "u2"}, wantOpen: 101700},
		{name: "full credit", billID: "b1", lines: []credit.Line{{FeeCents: 100000, TaxCents: 13000}}, by: credit.Approver{UserID: "u2"}, wantOpen: 0},
		{name: "more than open", billID: "b1", lines: []credit.Line{{FeeCents: 100000, TaxCents: 13001}}, draftErr: credit.ErrExceedsBalance},
		{name: "undelivered bill", billID: "draft", lines: []credit.Line{{FeeCents: 1}}, draftErr: credit.ErrBillNotDelivered},
		{name: "negative line", billID: "b1", lines: []credit.Line{{FeeCents: -1}}, draftErr: credit.ErrInvalidAmount},
		{name: "empty", billID: "b1", draftErr: credit.ErrInvalidAmount},
		{name: "unknown bill", billID: "nope", lines: []credit.Line{{FeeCents: 1}}, draftErr: credit.ErrNotFound},
		{name: "preparer approves", billID: "b1", lines: []credit.Line{{FeeCents: 1}}, by: credit.Approver{UserID: "u1"}, approveErr: credit.ErrSelfApproval, wantOpen: 113000},
		{name: "agent approves", billID: "b1", lines: []credit.Line{{FeeCents: 1}}, by: credit.Approver{UserID: "u2", IsAgent: true}, approveErr: credit.ErrAgentApproval, wantOpen: 113000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake()
			n, err := f.DraftNote(ctx, credit.Note{BillID: tt.billID, Lines: tt.lines, PreparedBy: "u1"})
			if !errors.Is(err, tt.draftErr) {
				t.Fatalf("draft err = %v, want %v", err, tt.draftErr)
			}
			if err != nil {
				return
			}
			got, err := f.ApproveNote(ctx, n.ID, tt.by)
			if !errors.Is(err, tt.approveErr) {
				t.Fatalf("approve err = %v, want %v", err, tt.approveErr)
			}
			if err == nil && (got.Number == "" || got.Status != credit.StatusApproved) {
				t.Fatalf("approved note = %+v", got)
			}
			b, _ := f.Bill(ctx, "b1")
			if b.OpenCents != tt.wantOpen {
				t.Fatalf("open = %d, want %d", b.OpenCents, tt.wantOpen)
			}
			if err == nil {
				if _, err := f.ApproveNote(ctx, n.ID, tt.by); !errors.Is(err, credit.ErrInvalidState) {
					t.Fatalf("second approve err = %v", err)
				}
			}
		})
	}
}

func TestWriteOff(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		amount     int64
		by         credit.Approver
		draftErr   error
		approveErr error
		wantOpen   int64
	}{
		{name: "write off rest", amount: 113000, by: credit.Approver{UserID: "u2"}, wantOpen: 0},
		{name: "zero", amount: 0, draftErr: credit.ErrInvalidAmount},
		{name: "too much", amount: 113001, draftErr: credit.ErrExceedsBalance},
		{name: "preparer approves", amount: 5, by: credit.Approver{UserID: "u1"}, approveErr: credit.ErrSelfApproval, wantOpen: 113000},
		{name: "agent approves", amount: 5, by: credit.Approver{UserID: "u3", IsAgent: true}, approveErr: credit.ErrAgentApproval, wantOpen: 113000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake()
			w, err := f.DraftWriteOff(ctx, credit.WriteOff{BillID: "b1", AmountCents: tt.amount, PreparedBy: "u1"})
			if !errors.Is(err, tt.draftErr) {
				t.Fatalf("draft err = %v, want %v", err, tt.draftErr)
			}
			if err != nil {
				return
			}
			if _, err := f.ApproveWriteOff(ctx, w.ID, tt.by); !errors.Is(err, tt.approveErr) {
				t.Fatalf("approve err = %v, want %v", err, tt.approveErr)
			}
			b, _ := f.Bill(ctx, "b1")
			if b.OpenCents != tt.wantOpen {
				t.Fatalf("open = %d, want %d", b.OpenCents, tt.wantOpen)
			}
		})
	}
}

func TestNotFound(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	tests := []struct {
		name string
		op   func() error
	}{
		{name: "note", op: func() error { _, err := f.Note(ctx, "x"); return err }},
		{name: "write-off", op: func() error { _, err := f.WriteOff(ctx, "x"); return err }},
		{name: "approve note", op: func() error { _, err := f.ApproveNote(ctx, "x", credit.Approver{UserID: "u2"}); return err }},
		{name: "approve write-off", op: func() error { _, err := f.ApproveWriteOff(ctx, "x", credit.Approver{UserID: "u2"}); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.op(); !errors.Is(err, credit.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}
