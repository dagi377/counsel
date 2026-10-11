package bill_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/bill/billfake"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
)

type nilRunner struct{}

func (nilRunner) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, nil)
}

// A message relayed twice delivers the bill once: the relay claims (consumer, message id).
func TestDeliverConsumerIdempotentByMessageID(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	f := billfake.New(func() time.Time { return now })
	if err := f.Put(bill.Bill{ID: "b1", MatterID: "m1", Status: bill.StatusPendingApproval, PreparedBy: "prep",
		FeesCents: 1000, TotalCents: 1000}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Approve(ctx, nil, bill.Approval{BillID: "b1", ApprovalID: "g1", ApprovedBy: "lawyer", ApprovedAt: now, Prefix: "INV"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Sign(ctx, nil, "b1", bill.Signature{SignedBy: "lawyer", SignedAt: now, Hash: "sha256:ab"}); err != nil {
		t.Fatal(err)
	}
	ob := outboxfake.New()
	body := bill.DeliveredBody{BillID: "b1", Channel: bill.ChannelPortal, AcceptedAt: now.Add(-time.Minute)}
	if err := ob.Emit(ctx, nil, registry.Subject{Kind: bill.SubjectBill, ID: "b1"}, bill.MessageDelivered, body); err != nil {
		t.Fatal(err)
	}
	d := &outbox.Deliverer{Relay: ob, Inbox: ob, DB: nilRunner{}, Consumers: []outbox.Consumer{bill.DeliverConsumer(f)}}
	msg := ob.Messages()[0]
	tests := []struct{ name string }{{"first relay"}, {"redelivery"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := ob.Claim(ctx, nil, "probe", msg.ID+tt.name)
			if err != nil || !first {
				t.Fatalf("probe claim: %v %v", first, err)
			}
			for _, c := range d.Consumers {
				if err := d.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
					ok, err := d.Inbox.Claim(ctx, tx, c.Name, msg.ID)
					if err != nil || !ok {
						return err
					}
					return c.Handle(ctx, tx, msg)
				}); err != nil {
					t.Fatalf("handle: %v", err)
				}
			}
			b, _ := f.Get(ctx, "b1")
			if b.Status != bill.StatusDelivered || !b.DeliveredAt.Equal(body.AcceptedAt) {
				t.Fatalf("bill status %s delivered %v", b.Status, b.DeliveredAt)
			}
		})
	}
}
