package billfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/bill"
)

var now = time.Date(2026, 10, 4, 12, 0, 0, 0, toronto)

func draft(id bill.ID) bill.Bill {
	return bill.Bill{
		ID: id, MatterID: "m1", PayerID: "p1", Status: bill.StatusPendingApproval, PreparedBy: "prep",
		FeesCents: 100000, TaxableDisbCents: 20000, NontaxableDisbCents: 7000, TaxCents: 15600, TotalCents: 142600,
		Lines: []bill.Line{{Ordinal: 1, Source: bill.SourceFlat, Narrative: "work", QuantityMilli: 1000, RateCents: 100000, AmountCents: 100000, TaxCode: "HST-ON", FeeBasis: "flat"}},
	}
}

func approval(id bill.ID, gateID string, at time.Time) bill.Approval {
	return bill.Approval{BillID: id, ApprovalID: gateID, ApprovedBy: "lawyer", ApprovedAt: at, Prefix: "INV"}
}

func newFake(t *testing.T, ids ...bill.ID) *Fake {
	t.Helper()
	f := New(func() time.Time { return now })
	for _, id := range ids {
		if err := f.Put(draft(id)); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func TestApprove(t *testing.T) {
	ctx := context.Background()
	lateEvening := time.Date(2026, 10, 3, 23, 30, 0, 0, toronto)
	tests := []struct {
		name       string
		steps      []bill.Approval
		wantErr    error
		wantNumber string
		wantIssue  string
	}{
		{name: "first number of year", steps: []bill.Approval{approval("b1", "g1", now)}, wantNumber: "INV-2026-0001", wantIssue: "2026-10-04"},
		{name: "issue date is Toronto date", steps: []bill.Approval{approval("b1", "g1", lateEvening.UTC())}, wantNumber: "INV-2026-0001", wantIssue: "2026-10-03"},
		{name: "gapless across bills", steps: []bill.Approval{approval("b2", "g2", now), approval("b1", "g1", now)}, wantNumber: "INV-2026-0002", wantIssue: "2026-10-04"},
		{name: "replay is a no-op", steps: []bill.Approval{approval("b1", "g1", now), approval("b1", "g1", now), approval("b1", "g1", now)}, wantNumber: "INV-2026-0001", wantIssue: "2026-10-04"},
		{name: "failed approval uses no number", steps: []bill.Approval{{BillID: "b2", ApprovalID: "g2", ApprovedBy: "prep", ApprovedAt: now, Prefix: "INV"}, approval("b1", "g1", now)}, wantNumber: "INV-2026-0001", wantIssue: "2026-10-04"},
		{name: "new year restarts", steps: []bill.Approval{approval("b2", "g2", now), approval("b1", "g1", time.Date(2027, 1, 1, 9, 0, 0, 0, toronto))}, wantNumber: "INV-2027-0001", wantIssue: "2027-01-01"},
		{name: "preparer never approves", steps: []bill.Approval{{BillID: "b1", ApprovalID: "g1", ApprovedBy: "prep", ApprovedAt: now, Prefix: "INV"}}, wantErr: bill.ErrPreparerApproves},
		{name: "second approval refused", steps: []bill.Approval{approval("b1", "g1", now), approval("b1", "g9", now)}, wantErr: bill.ErrNotPending},
		{name: "unknown bill", steps: []bill.Approval{approval("zz", "g1", now)}, wantErr: bill.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t, "b1", "b2")
			var err error
			for _, a := range tt.steps {
				_, err = f.Approve(ctx, nil, a)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Approve err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			b, _ := f.Get(ctx, "b1")
			if b.Number != tt.wantNumber || b.IssueDate.Format(time.DateOnly) != tt.wantIssue || b.Status != bill.StatusApproved {
				t.Fatalf("bill number %s issue %s status %s", b.Number, b.IssueDate.Format(time.DateOnly), b.Status)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	ctx := context.Background()
	disc, rate, due := int64(12000), int64(30000), now.AddDate(0, 1, 0)
	tooBig := int64(120001)
	tests := []struct {
		name      string
		approved  bool
		change    bill.Change
		want      error
		wantTotal int64
	}{
		{name: "discount on draft", change: bill.Change{DiscountCents: &disc}, wantTotal: 130600},
		{name: "discount beyond fees and taxable disbursements", change: bill.Change{DiscountCents: &tooBig}, want: bill.ErrInvalid, wantTotal: 142600},
		{name: "discount after approval", approved: true, change: bill.Change{DiscountCents: &disc}, want: bill.ErrUseCreditNote, wantTotal: 142600},
		{name: "interest frozen after approval", approved: true, change: bill.Change{InterestRatePPM: &rate}, want: bill.ErrBillGuard, wantTotal: 142600},
		{name: "due date frozen after approval", approved: true, change: bill.Change{DueDate: &due}, want: bill.ErrBillGuard, wantTotal: 142600},
		{name: "lines frozen after approval", approved: true, change: bill.Change{Lines: []bill.Line{}}, want: bill.ErrBillGuard, wantTotal: 142600},
		{name: "delivered_at is read-only", change: bill.Change{DeliveredAt: &now}, want: bill.ErrReadOnlyField, wantTotal: 142600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t, "b1")
			if tt.approved {
				if _, err := f.Approve(ctx, nil, approval("b1", "g1", now)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.Update(ctx, nil, "b1", tt.change); !errors.Is(err, tt.want) {
				t.Fatalf("Update err = %v, want %v", err, tt.want)
			}
			if b, _ := f.Get(ctx, "b1"); b.TotalCents != tt.wantTotal {
				t.Fatalf("total = %d, want %d", b.TotalCents, tt.wantTotal)
			}
		})
	}
}

func TestDeliver(t *testing.T) {
	ctx := context.Background()
	accepted := time.Date(2026, 10, 3, 23, 30, 0, 0, time.FixedZone("EDT", -4*3600))
	sig := bill.Signature{SignedBy: "lawyer", SignedAt: now, Hash: "sha256:ab"}
	tests := []struct {
		name          string
		approve, sign bool
		ev            bill.DeliveryEvidence
		want          error
		wantOn        string
	}{
		{name: "email accepted late evening", approve: true, sign: true, ev: bill.DeliveryEvidence{Channel: bill.ChannelEmail, AcceptedAt: accepted}, wantOn: "2026-10-03"},
		{name: "no signature", approve: true, ev: bill.DeliveryEvidence{Channel: bill.ChannelEmail, AcceptedAt: accepted}, want: bill.ErrSignatureRequired},
		{name: "not approved", ev: bill.DeliveryEvidence{Channel: bill.ChannelEmail, AcceptedAt: accepted}, want: bill.ErrNotApproved},
		{name: "bounce leaves it undelivered", approve: true, sign: true, ev: bill.DeliveryEvidence{Channel: bill.ChannelEmail, AcceptedAt: accepted, Bounced: true}},
		{name: "future evidence", approve: true, sign: true, ev: bill.DeliveryEvidence{Channel: bill.ChannelEmail, AcceptedAt: now.Add(time.Hour)}, want: bill.ErrFutureEvidence},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t, "b1")
			if tt.approve {
				if _, err := f.Approve(ctx, nil, approval("b1", "g1", now)); err != nil {
					t.Fatal(err)
				}
			}
			if tt.sign {
				if err := f.Sign(ctx, nil, "b1", sig); err != nil {
					t.Fatal(err)
				}
			}
			b, err := f.Deliver(ctx, nil, "b1", tt.ev)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Deliver err = %v, want %v", err, tt.want)
			}
			if err != nil {
				return
			}
			gotOn := ""
			if !b.DeliveredAt.IsZero() {
				gotOn = b.DeliveredOn.Format(time.DateOnly)
			}
			if gotOn != tt.wantOn {
				t.Fatalf("delivered on %q, want %q", gotOn, tt.wantOn)
			}
			if tt.wantOn != "" {
				if _, err := f.Deliver(ctx, nil, "b1", tt.ev); !errors.Is(err, bill.ErrAlreadyDelivered) {
					t.Fatalf("second Deliver err = %v", err)
				}
				if err := f.Sign(ctx, nil, "b1", sig); err == nil {
					t.Fatal("re-sign after delivery succeeded")
				}
			}
		})
	}
}

func TestPut(t *testing.T) {
	tests := []struct {
		name string
		mod  func(b *bill.Bill)
		want error
	}{
		{name: "balanced", mod: func(*bill.Bill) {}},
		{name: "total off by one", mod: func(b *bill.Bill) { b.TotalCents++ }, want: bill.ErrInvalid},
		{name: "no id", mod: func(b *bill.Bill) { b.ID = "" }, want: bill.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := draft("b1")
			tt.mod(&b)
			if err := New(time.Now).Put(b); !errors.Is(err, tt.want) {
				t.Fatalf("Put err = %v, want %v", err, tt.want)
			}
		})
	}
}
