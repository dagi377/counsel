package trust

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// Each publisher writes exactly one outbox event of its type with ids-only keys; a body
// missing a required id publishes nothing (EVENTS-9).
func TestARC_I_030_TrustEventPublishers(t *testing.T) {
	tests := []struct {
		name    string
		publish func(context.Context, *outboxfake.Fake) error
		typ     string
		subject string
		keys    []string
		wantErr error
	}{
		{"trust.ledger_below_floor", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishLedgerBelowFloor(ctx, em, nil, LedgerBelowFloor{MatterID: "matter-id-1", UserID: "user-id-1"})
		}, EventLedgerBelowFloor, "matter-id-1", []string{"matter_id", "user_id"}, nil},
		{"trust.ledger_below_floor without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishLedgerBelowFloor(ctx, em, nil, LedgerBelowFloor{})
		}, "", "", nil, ErrEventBody},
		{"trust.statement_received", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishStatementReceived(ctx, em, nil, StatementReceived{StatementID: "statement-id-1", AccountID: "account-id-1", UserID: "user-id-1"})
		}, EventStatementReceived, "statement-id-1", []string{"account_id", "statement_id", "user_id"}, nil},
		{"trust.statement_received without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishStatementReceived(ctx, em, nil, StatementReceived{})
		}, "", "", nil, ErrEventBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			if err := tt.publish(context.Background(), em); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			msgs := em.Messages()
			if tt.wantErr != nil {
				if len(msgs) != 0 {
					t.Fatalf("events = %d, want 0", len(msgs))
				}
				return
			}
			if len(msgs) != 1 || msgs[0].Type != tt.typ || msgs[0].Subject.ID != tt.subject {
				t.Fatalf("events = %+v, want one %s on %s", msgs, tt.typ, tt.subject)
			}
			var body map[string]any
			if err := json.Unmarshal(msgs[0].Payload, &body); err != nil {
				t.Fatal(err)
			}
			var keys []string
			for k := range body {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, tt.keys) {
				t.Fatalf("payload keys = %v, want %v", keys, tt.keys)
			}
		})
	}
}
