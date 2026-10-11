package credit

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// rule: BIL-C-205
// Q-INTEG-12.1: after a trust fee transfer the unpaid balance is apportioned over fees,
// disbursements and tax in the bill's own proportions; the transfer is never re-split.
func TestDraftOpenBuckets(t *testing.T) {
	bill := WriteOffBuckets{Fee: 90000, Dtx: 10000, Dnt: 5000, Tax: 13000} // 118000
	tests := []struct {
		name              string
		open, issued      WriteOffBuckets
		balance, transfer int64
		want              WriteOffBuckets
		wantErr           error
	}{
		{name: "no transfer keeps the buckets", open: bill, issued: bill, balance: 118000, want: bill},
		{name: "no transfer, unattributed reduction", open: bill, issued: bill, balance: 100000, wantErr: ErrInvalidState},
		{name: "half paid from trust", open: bill, issued: bill, balance: 59000, transfer: 59000,
			want: WriteOffBuckets{Fee: 45000, Dtx: 5000, Dnt: 2500, Tax: 6500}},
		{name: "odd cents by largest remainder", open: bill, issued: bill, balance: 1001, transfer: 116999,
			want: WriteOffBuckets{Fee: 764, Dtx: 85, Dnt: 42, Tax: 110}},
		{name: "fully paid from trust", open: bill, issued: bill, balance: 0, transfer: 118000, want: WriteOffBuckets{}},
		{name: "open interest stays its own bucket",
			open:   WriteOffBuckets{Fee: 90000, Dtx: 10000, Dnt: 5000, Tax: 13000, Interest: 500},
			issued: bill, balance: 59500, transfer: 59000,
			want: WriteOffBuckets{Fee: 45000, Dtx: 5000, Dnt: 2500, Tax: 6500, Interest: 500}},
		{name: "transfer does not match the balance", open: bill, issued: bill, balance: 60000, transfer: 59000, wantErr: ErrInvalidState},
		{name: "negative transfer", open: bill, issued: bill, balance: 118001, transfer: -1, wantErr: ErrInvalidState},
		{name: "apportioned bucket above outstanding",
			open:   WriteOffBuckets{Fee: 90000, Dtx: 0, Dnt: 5000, Tax: 13000},
			issued: bill, balance: 58000, transfer: 50000, wantErr: ErrInvalidState},
		{name: "bill with no components", open: WriteOffBuckets{Fee: 100}, issued: WriteOffBuckets{}, balance: 50, transfer: 50,
			wantErr: ErrInvalidState},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DraftOpenBuckets(tt.open, tt.issued, tt.balance, tt.transfer)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("DraftOpenBuckets err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("DraftOpenBuckets = %+v, want %+v", got, tt.want)
			}
			if err == nil && got.Sum() != tt.balance {
				t.Fatalf("buckets sum %d, want balance %d", got.Sum(), tt.balance)
			}
		})
	}
}

// rule: BIL-C-205
func TestDraftWriteOffAfterTrustTransfer(t *testing.T) {
	tx := newDraftTx()
	b := tx.bills["b1"]
	b.issued, b.balance, b.transfer = b.outstanding, 59000, 59000
	tx.bills["b1"] = b
	w, err := PGWriteOffs{}.Draft(context.Background(), tx, WriteOff{BillID: "b1", AmountCents: 59000, Reason: "uncollectible", PreparedBy: "clerk"})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	want := WriteOffBuckets{Fee: 45000, Dtx: 5000, Dnt: 2500, Tax: 6500}
	if got := tx.drafts[w.ID].split; got != want {
		t.Fatalf("split = %+v, want %+v", got, want)
	}
}

// rule: BIL-C-085
// A rejected or withdrawn draft is final and leaves the second-approval total.
func TestCloseWriteOff(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		to         Status
		creditGate string
		wantErr    error
	}{
		{name: "gate rejected", id: "w1", to: StatusRejected, creditGate: "gb5"},
		{name: "preparer withdraws", id: "w1", to: StatusWithdrawn, creditGate: "gb5"},
		{name: "not submitted", id: "w1", to: StatusRejected, wantErr: ErrInvalidState},
		{name: "approve is not a close", id: "w1", to: StatusApproved, creditGate: "gb5", wantErr: ErrInvalidState},
		{name: "draft is not a close", id: "w1", to: StatusDraft, creditGate: "gb5", wantErr: ErrInvalidState},
		{name: "missing draft", id: "nope", to: StatusRejected, creditGate: "gb5", wantErr: ErrInvalidState},
		{name: "no id", to: StatusRejected, creditGate: "gb5", wantErr: ErrInvalidState},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newDraftTx()
			tx.drafts["w1"].creditGate = tt.creditGate
			err := PGWriteOffs{}.Close(context.Background(), tx, tt.id, tt.to)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Close err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			got, err := PGWriteOffs{}.Lock(context.Background(), tx, "w1")
			if err != nil || got.Status != tt.to {
				t.Fatalf("Lock = %+v, %v; want status %s", got, err, tt.to)
			}
			// A closed draft is final: it cannot be closed again or submitted.
			if err := (PGWriteOffs{}).Close(context.Background(), tx, "w1", StatusWithdrawn); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("second Close err = %v, want ErrInvalidState", err)
			}
		})
	}
}

// rule: BIL-C-085
// Only status draft counts as pending, so rejected and withdrawn drafts never reach the total.
func TestPendingTotalsCountOnlyDrafts(t *testing.T) {
	if !strings.Contains(pendingWriteOffsSQL, "status = 'draft'") {
		t.Fatalf("pendingWriteOffsSQL must count only status draft: %s", pendingWriteOffsSQL)
	}
	for _, st := range []Status{StatusRejected, StatusWithdrawn} {
		if strings.Contains(pendingWriteOffsSQL, string(st)) {
			t.Fatalf("pendingWriteOffsSQL counts %s", st)
		}
	}
}
