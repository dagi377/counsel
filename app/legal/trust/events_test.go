package trust

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

func TestTrustEventsPublishOne(t *testing.T) {
	stmt := StatementReceived{StatementID: "s-1", AccountID: "a-1", PeriodStart: "2026-09-01", PeriodEnd: "2026-09-30"}
	low := LedgerBelowFloor{LedgerID: "l-1", MatterID: "m-1", BalanceCents: 49_999, FloorCents: 50_000}
	tests := []struct {
		name    string
		publish func(Events) error
		typ, id string
		wantErr error
	}{
		{"statement received", func(e Events) error { return e.StatementReceived(context.Background(), nil, stmt) }, EventStatementReceived, "s-1", nil},
		{"ledger below floor", func(e Events) error { return e.LedgerBelowFloor(context.Background(), nil, low) }, EventLedgerBelowFloor, "l-1", nil},
		{"statement without period", func(e Events) error {
			return e.StatementReceived(context.Background(), nil, StatementReceived{StatementID: "s-1", AccountID: "a-1"})
		}, "", "", ErrEventInvalid},
		{"balance at floor is not below", func(e Events) error {
			b := low
			b.BalanceCents = b.FloorCents
			return e.LedgerBelowFloor(context.Background(), nil, b)
		}, "", "", ErrEventInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := outboxfake.New()
			err := tt.publish(Events{Emitter: f})
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			msgs := f.Messages()
			if tt.wantErr != nil {
				if len(msgs) != 0 {
					t.Fatalf("published %d events on error", len(msgs))
				}
				return
			}
			if len(msgs) != 1 || msgs[0].Type != tt.typ || msgs[0].Subject.ID != tt.id {
				t.Fatalf("messages = %+v, want one %s on %s", msgs, tt.typ, tt.id)
			}
		})
	}
	if err := (Events{}).StatementReceived(context.Background(), nil, stmt); !errors.Is(err, ErrNoEventEmitter) {
		t.Fatalf("no emitter: err = %v", err)
	}
}
