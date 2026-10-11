package paymentfake

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/billing/payment"
)

func TestCapture(t *testing.T) {
	tests := []struct {
		name    string
		c       payment.Capture
		want    []payment.Allocation
		wantErr error
	}{
		{
			name: "partial tax share",
			c:    payment.Capture{ClientID: "c1", BillID: "b1", Kind: payment.KindBillPayment, AmountCents: 50000},
			want: []payment.Allocation{{BillID: "b1", FeeCents: 44248, TaxCents: 5752}},
		},
		{
			name: "overpayment unapplied",
			c:    payment.Capture{ClientID: "c1", BillID: "b1", Kind: payment.KindBillPayment, AmountCents: 100000},
			want: []payment.Allocation{{BillID: "b1", FeeCents: 80000, TaxCents: 10400}, {FeeCents: 9600}},
		},
		{name: "retainer", c: payment.Capture{ClientID: "c1", BillID: "b1", Kind: payment.KindRetainer, AmountCents: 100}, wantErr: payment.ErrRetainerToOperating},
		{name: "other client", c: payment.Capture{ClientID: "c2", BillID: "b1", AmountCents: 100}, wantErr: payment.ErrOtherClient},
		{name: "zero", c: payment.Capture{ClientID: "c1", BillID: "b1"}, wantErr: payment.ErrInvalid},
		{name: "unknown bill", c: payment.Capture{ClientID: "c1", BillID: "zz", AmountCents: 1}, wantErr: payment.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			f.AddBill("b1", "c1", 80000, 10400)
			p, got, err := f.Capture(context.Background(), tt.c)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Capture err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("allocations = %+v, want %+v", got, tt.want)
			}
			var sum int64
			for i, a := range got {
				w := tt.want[i]
				w.PaymentID = p.ID
				if a != w {
					t.Errorf("allocation %d = %+v, want %+v", i, a, w)
				}
				sum += a.Total()
			}
			if sum != tt.c.AmountCents {
				t.Errorf("sum(allocations) = %d, want %d", sum, tt.c.AmountCents)
			}
		})
	}
}

func TestCaptureDedup(t *testing.T) {
	tests := []struct {
		name string
		c    payment.Capture
	}{
		{name: "provider event", c: payment.Capture{ClientID: "c1", BillID: "b1", AmountCents: 1000, Provider: "card", ProviderEventID: "evt_1"}},
		{name: "idempotency key", c: payment.Capture{ClientID: "c1", BillID: "b1", AmountCents: 1000, IdempotencyKey: "k1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			f.AddBill("b1", "c1", 80000, 10400)
			a, _, _ := f.Capture(context.Background(), tt.c)
			b, _, _ := f.Capture(context.Background(), tt.c)
			if a.ID != b.ID {
				t.Fatalf("two payments %s and %s, want one", a.ID, b.ID)
			}
			if fee, tax := f.Open("b1"); fee+tax != 90400-1000 {
				t.Fatalf("open = %d, want %d", fee+tax, 90400-1000)
			}
		})
	}
}

func TestReverseRestoresBuckets(t *testing.T) {
	tests := []struct {
		name   string
		reason payment.ReversalReason
	}{
		{name: "nsf", reason: payment.ReasonNSF},
		{name: "chargeback", reason: payment.ReasonChargeback},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New()
			f.AddBill("b1", "c1", 80000, 10400)
			p, _, err := f.Capture(ctx, payment.Capture{ClientID: "c1", BillID: "b1", AmountCents: 50000})
			if err != nil {
				t.Fatal(err)
			}
			got, err := f.Reverse(ctx, p.ID, tt.reason)
			if err != nil || got.Status != payment.StatusReversed || got.Reason != tt.reason {
				t.Fatalf("Reverse = %+v, %v", got, err)
			}
			if fee, tax := f.Open("b1"); fee != 80000 || tax != 10400 {
				t.Fatalf("open = %d/%d, want 80000/10400", fee, tax)
			}
			if _, err := f.Reverse(ctx, p.ID, tt.reason); !errors.Is(err, payment.ErrReversed) {
				t.Fatalf("second Reverse err = %v, want ErrReversed", err)
			}
			if _, err := f.Get(ctx, "nope"); !errors.Is(err, payment.ErrNotFound) {
				t.Fatalf("Get err = %v", err)
			}
			if as, err := f.Allocations(ctx, p.ID); err != nil || len(as) != 1 {
				t.Fatalf("Allocations = %v, %v", as, err)
			}
		})
	}
}
