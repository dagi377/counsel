package billing

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
func TestARC_I_030_BillingEventPublishers(t *testing.T) {
	tests := []struct {
		name    string
		publish func(context.Context, *outboxfake.Fake) error
		typ     string
		subject string
		keys    []string
		wantErr error
	}{
		{"gate.decided", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishGateDecided(ctx, em, nil, GateDecided{GateID: "gate-id-1", Version: 1, Decision: "decision-1", DeciderID: "decider-id-1", Final: true, UserID: "user-id-1"})
		}, EventGateDecided, "gate-id-1", []string{"decider_id", "decision", "final", "gate_id", "user_id", "version"}, nil},
		{"gate.decided without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishGateDecided(ctx, em, nil, GateDecided{})
		}, "", "", nil, ErrEventBody},
		{"payment.received", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishPaymentReceived(ctx, em, nil, PaymentReceived{PaymentID: "payment-id-1", AmountCents: 1, Method: "method-1", Account: "account-1", ProviderRef: "provider-ref-1", UserID: "user-id-1"})
		}, EventPaymentReceived, "payment-id-1", []string{"account", "amount_cents", "method", "payment_id", "provider_ref", "user_id"}, nil},
		{"payment.received without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishPaymentReceived(ctx, em, nil, PaymentReceived{})
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
