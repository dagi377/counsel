package intake

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
func TestARC_I_030_IntakeEventPublishers(t *testing.T) {
	tests := []struct {
		name    string
		publish func(context.Context, *outboxfake.Fake) error
		typ     string
		subject string
		keys    []string
		wantErr error
	}{
		{"consultation.dictation_uploaded", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishDictationUploaded(ctx, em, nil, DictationUploaded{ConsultationID: "consultation-id-1", DocumentID: "document-id-1", UserID: "user-id-1"})
		}, EventDictationUploaded, "consultation-id-1", []string{"consultation_id", "document_id", "user_id"}, nil},
		{"consultation.dictation_uploaded without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishDictationUploaded(ctx, em, nil, DictationUploaded{})
		}, "", "", nil, ErrEventBody},
		{"consultation.ended", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishConsultationEnded(ctx, em, nil, ConsultationEnded{ConsultationID: "consultation-id-1", LeadID: "lead-id-1", UserID: "user-id-1"})
		}, EventConsultationEnded, "consultation-id-1", []string{"consultation_id", "lead_id", "user_id"}, nil},
		{"consultation.ended without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishConsultationEnded(ctx, em, nil, ConsultationEnded{})
		}, "", "", nil, ErrEventBody},
		{"consultation.retained", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishConsultationRetained(ctx, em, nil, ConsultationRetained{ConsultationID: "consultation-id-1", LeadID: "lead-id-1", UserID: "user-id-1"})
		}, EventConsultRetained, "consultation-id-1", []string{"consultation_id", "lead_id", "user_id"}, nil},
		{"consultation.retained without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishConsultationRetained(ctx, em, nil, ConsultationRetained{})
		}, "", "", nil, ErrEventBody},
		{"lead.declined", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishLeadDeclinedEvent(ctx, em, nil, LeadDeclinedEvent{LeadID: "lead-id-1", ReasonCode: "reason-code-1", UserID: "user-id-1"})
		}, EventLeadDeclined, "lead-id-1", []string{"lead_id", "reason_code", "user_id"}, nil},
		{"lead.declined without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishLeadDeclinedEvent(ctx, em, nil, LeadDeclinedEvent{})
		}, "", "", nil, ErrEventBody},
		{"lead.referred", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishLeadReferredEvent(ctx, em, nil, LeadReferredEvent{LeadID: "lead-id-1", ReasonCode: "reason-code-1", UserID: "user-id-1"})
		}, EventLeadReferred, "lead-id-1", []string{"lead_id", "reason_code", "user_id"}, nil},
		{"lead.referred without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishLeadReferredEvent(ctx, em, nil, LeadReferredEvent{})
		}, "", "", nil, ErrEventBody},
		{"receptionist_conversation.started", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishReceptionistConversationStarted(ctx, em, nil, ReceptionistConversationStarted{ConversationID: "conversation-id-1", Channel: "channel-1", LeadID: "lead-id-1", UserID: "user-id-1"})
		}, EventReceptionistConversationStarted, "conversation-id-1", []string{"channel", "conversation_id", "lead_id", "user_id"}, nil},
		{"receptionist_conversation.started without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishReceptionistConversationStarted(ctx, em, nil, ReceptionistConversationStarted{})
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
