package matter

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
func TestARC_I_030_MatterEventPublishers(t *testing.T) {
	tests := []struct {
		name    string
		publish func(context.Context, *outboxfake.Fake) error
		typ     string
		subject string
		keys    []string
		wantErr error
	}{
		{"intake.lead_ready_to_open", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishLeadReadyToOpen(ctx, em, nil, LeadReadyToOpen{LeadID: "lead-id-1", UserID: "user-id-1"})
		}, EventLeadReadyToOpen, "lead-id-1", []string{"lead_id", "user_id"}, nil},
		{"intake.lead_ready_to_open without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishLeadReadyToOpen(ctx, em, nil, LeadReadyToOpen{})
		}, "", "", nil, ErrEventBody},
		{"matter.call_transcript_filed", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishCallTranscriptFiled(ctx, em, nil, CallTranscriptFiled{MatterID: "matter-id-1", DocumentID: "document-id-1", UserID: "user-id-1"})
		}, EventCallTranscriptFiled, "matter-id-1", []string{"document_id", "matter_id", "user_id"}, nil},
		{"matter.call_transcript_filed without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishCallTranscriptFiled(ctx, em, nil, CallTranscriptFiled{})
		}, "", "", nil, ErrEventBody},
		{"matter.closed", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishMatterClosed(ctx, em, nil, MatterClosed{MatterID: "matter-id-1", ClosedOn: "closed-on-1", GateID: "gate-id-1", UserID: "user-id-1"})
		}, EventClosed, "matter-id-1", []string{"closed_on", "gate_id", "matter_id", "user_id"}, nil},
		{"matter.closed without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishMatterClosed(ctx, em, nil, MatterClosed{})
		}, "", "", nil, ErrEventBody},
		{"matter.team_changed", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishTeamChanged(ctx, em, nil, TeamChanged{MatterID: "matter-id-1", AddedUserIDs: []string{"added-user-ids-1"}, RemovedUserIDs: []string{"removed-user-ids-1"}, GateID: "gate-id-1", UserID: "user-id-1"})
		}, EventTeamChanged, "matter-id-1", []string{"added_user_ids", "gate_id", "matter_id", "removed_user_ids", "user_id"}, nil},
		{"matter.team_changed without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishTeamChanged(ctx, em, nil, TeamChanged{})
		}, "", "", nil, ErrEventBody},
		{"matter.updated", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishMatterUpdated(ctx, em, nil, MatterUpdated{MatterID: "matter-id-1", ChangedFields: []string{"changed-fields-1"}, Version: 1, UserID: "user-id-1"})
		}, EventUpdated, "matter-id-1", []string{"changed_fields", "matter_id", "user_id", "version"}, nil},
		{"matter.updated without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishMatterUpdated(ctx, em, nil, MatterUpdated{})
		}, "", "", nil, ErrEventBody},
		{"timekeeping.wip_changed", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishWIPChanged(ctx, em, nil, WIPChanged{MatterID: "matter-id-1", UserID: "user-id-1"})
		}, EventWIPChanged, "matter-id-1", []string{"matter_id", "user_id"}, nil},
		{"timekeeping.wip_changed without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishWIPChanged(ctx, em, nil, WIPChanged{})
		}, "", "", nil, ErrEventBody},
		{"trust.balance_changed", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishTrustBalanceChanged(ctx, em, nil, TrustBalanceChanged{MatterID: "matter-id-1", UserID: "user-id-1"})
		}, EventTrustBalanceChanged, "matter-id-1", []string{"matter_id", "user_id"}, nil},
		{"trust.balance_changed without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishTrustBalanceChanged(ctx, em, nil, TrustBalanceChanged{})
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
