package docs

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
func TestARC_I_030_DocsEventPublishers(t *testing.T) {
	tests := []struct {
		name    string
		publish func(context.Context, *outboxfake.Fake) error
		typ     string
		subject string
		keys    []string
		wantErr error
	}{
		{"conflict.cleared", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishConflictCleared(ctx, em, nil, ConflictCleared{CheckID: "check-id-1", Outcome: "outcome-1", GateID: "gate-id-1", UserID: "user-id-1"})
		}, EventConflictCleared, "check-id-1", []string{"check_id", "gate_id", "outcome", "user_id"}, nil},
		{"conflict.cleared without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishConflictCleared(ctx, em, nil, ConflictCleared{})
		}, "", "", nil, ErrEventBody},
		{"document.draft_created", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishDraftCreated(ctx, em, nil, DraftCreated{DraftID: "draft-id-1", MatterID: "matter-id-1", Origin: "origin-1", AgentRunID: "agent-run-id-1", CitationCount: 1, UserID: "user-id-1"})
		}, EventDraftCreated, "draft-id-1", []string{"agent_run_id", "citation_count", "draft_id", "matter_id", "origin", "user_id"}, nil},
		{"document.draft_created without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishDraftCreated(ctx, em, nil, DraftCreated{})
		}, "", "", nil, ErrEventBody},
		{"document.released", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishDocumentReleased(ctx, em, nil, DocumentReleased{DocumentID: "document-id-1", VersionID: "version-id-1", Channel: "channel-1", RecipientContactIDs: []string{"recipient-contact-ids-1"}, GateID: "gate-id-1", UserID: "user-id-1"})
		}, EventDocumentReleased, "document-id-1", []string{"channel", "document_id", "gate_id", "recipient_contact_ids", "user_id", "version_id"}, nil},
		{"document.released without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishDocumentReleased(ctx, em, nil, DocumentReleased{})
		}, "", "", nil, ErrEventBody},
		{"document.uploaded", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishDocumentUploaded(ctx, em, nil, DocumentUploaded{DocumentID: "document-id-1", VersionID: "version-id-1", MatterID: "matter-id-1", SHA256: "sha256-1", Bytes: 1, Mime: "mime-1", UserID: "user-id-1"})
		}, EventDocumentUploaded, "document-id-1", []string{"bytes", "document_id", "matter_id", "mime", "sha256", "user_id", "version_id"}, nil},
		{"document.uploaded without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishDocumentUploaded(ctx, em, nil, DocumentUploaded{})
		}, "", "", nil, ErrEventBody},
		{"lead.converted", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishLeadConverted(ctx, em, nil, LeadConverted{LeadID: "lead-id-1", MatterID: "matter-id-1", GateID: "gate-id-1", UserID: "user-id-1"})
		}, EventLeadConverted, "lead-id-1", []string{"gate_id", "lead_id", "matter_id", "user_id"}, nil},
		{"lead.converted without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishLeadConverted(ctx, em, nil, LeadConverted{})
		}, "", "", nil, ErrEventBody},
		{"work_item.created", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishWorkItemCreated(ctx, em, nil, WorkItemCreated{WorkItemID: "work-item-id-1", MatterID: "matter-id-1", AssigneeID: "assignee-id-1", Origin: "origin-1", UserID: "user-id-1"})
		}, EventWorkItemCreated, "work-item-id-1", []string{"assignee_id", "matter_id", "origin", "user_id", "work_item_id"}, nil},
		{"work_item.created without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishWorkItemCreated(ctx, em, nil, WorkItemCreated{})
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
