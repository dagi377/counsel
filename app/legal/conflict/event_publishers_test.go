package conflict

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
func TestARC_I_030_ConflictEventPublishers(t *testing.T) {
	tests := []struct {
		name    string
		publish func(context.Context, *outboxfake.Fake) error
		typ     string
		subject string
		keys    []string
		wantErr error
	}{
		{"conflict.check_completed", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishCheckCompleted(ctx, em, nil, CheckCompleted{CheckID: "check-id-1", MatterID: "matter-id-1", LeadID: "lead-id-1", HitCount: 1, UserID: "user-id-1"})
		}, EventCheckCompleted, "check-id-1", []string{"check_id", "hit_count", "lead_id", "matter_id", "user_id"}, nil},
		{"conflict.check_completed without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishCheckCompleted(ctx, em, nil, CheckCompleted{})
		}, "", "", nil, ErrEventBody},
		{"conflict.clearance_subject_to_consent", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishClearanceSubjectToConsent(ctx, em, nil, ClearanceSubjectToConsent{CheckID: "check-id-1", GateID: "gate-id-1", UserID: "user-id-1"})
		}, EventClearanceSubjectToConsent, "check-id-1", []string{"check_id", "gate_id", "user_id"}, nil},
		{"conflict.clearance_subject_to_consent without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishClearanceSubjectToConsent(ctx, em, nil, ClearanceSubjectToConsent{})
		}, "", "", nil, ErrEventBody},
		{"trust.funds_expected", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishTrustFundsExpected(ctx, em, nil, TrustFundsExpected{RequestID: "request-id-1", MatterID: "matter-id-1", UserID: "user-id-1"})
		}, EventTrustFundsExpected, "request-id-1", []string{"matter_id", "request_id", "user_id"}, nil},
		{"trust.funds_expected without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishTrustFundsExpected(ctx, em, nil, TrustFundsExpected{})
		}, "", "", nil, ErrEventBody},
		{"trust.request_drafted", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishTrustRequestDrafted(ctx, em, nil, TrustRequestDrafted{RequestID: "request-id-1", MatterID: "matter-id-1", UserID: "user-id-1"})
		}, EventTrustRequestDrafted, "request-id-1", []string{"matter_id", "request_id", "user_id"}, nil},
		{"trust.request_drafted without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishTrustRequestDrafted(ctx, em, nil, TrustRequestDrafted{})
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
