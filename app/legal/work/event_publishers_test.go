package work

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
func TestARC_I_030_WorkEventPublishers(t *testing.T) {
	tests := []struct {
		name    string
		publish func(context.Context, *outboxfake.Fake) error
		typ     string
		subject string
		keys    []string
		wantErr error
	}{
		{"matter.key_date_changed", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishKeyDateChanged(ctx, em, nil, KeyDateChanged{MatterID: "matter-id-1", KeyDateID: "key-date-id-1", UserID: "user-id-1"})
		}, EventKeyDateChanged, "matter-id-1", []string{"key_date_id", "matter_id", "user_id"}, nil},
		{"matter.key_date_changed without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishKeyDateChanged(ctx, em, nil, KeyDateChanged{})
		}, "", "", nil, ErrEventBody},
		{"matter.template_applied", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishTemplateApplied(ctx, em, nil, TemplateApplied{MatterID: "matter-id-1", TemplateID: "template-id-1", UserID: "user-id-1"})
		}, EventTemplateApplied, "matter-id-1", []string{"matter_id", "template_id", "user_id"}, nil},
		{"matter.template_applied without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishTemplateApplied(ctx, em, nil, TemplateApplied{})
		}, "", "", nil, ErrEventBody},
		{"work.item_agent_ready", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishItemAgentReady(ctx, em, nil, ItemAgentReady{WorkItemID: "work-item-id-1", UserID: "user-id-1"})
		}, EventItemAgentReady, "work-item-id-1", []string{"user_id", "work_item_id"}, nil},
		{"work.item_agent_ready without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishItemAgentReady(ctx, em, nil, ItemAgentReady{})
		}, "", "", nil, ErrEventBody},
		{"work.item_assigned_to_agent", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishItemAssignedToAgent(ctx, em, nil, ItemAssignedToAgent{WorkItemID: "work-item-id-1", UserID: "user-id-1"})
		}, EventItemAssignedToAgent, "work-item-id-1", []string{"user_id", "work_item_id"}, nil},
		{"work.item_assigned_to_agent without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishItemAssignedToAgent(ctx, em, nil, ItemAssignedToAgent{})
		}, "", "", nil, ErrEventBody},
		{"work.workflow_draft_saved", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishWorkflowDraftSaved(ctx, em, nil, WorkflowDraftSaved{WorkflowID: "workflow-id-1", UserID: "user-id-1"})
		}, EventWorkflowDraftSaved, "workflow-id-1", []string{"user_id", "workflow_id"}, nil},
		{"work.workflow_draft_saved without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishWorkflowDraftSaved(ctx, em, nil, WorkflowDraftSaved{})
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
