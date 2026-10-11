// Minimal publishers for the kernel events this package's agent features bind to but no
// write path publishes yet (EVENTS-9, docs/QUESTIONS.md Q-EVENTS-9). The owning write path
// calls its publisher in the same transaction when it lands. Bodies carry ids, days, counts
// and codes only, plus user_id: the user an event run acts for (trigger.PayloadActor).

package work

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrEventBody means an event body lacks a required id; nothing is published.
var ErrEventBody = errors.New("work: event body missing a required field")

// KeyDateChanged is the body of EventKeyDateChanged: a matter key date changed.
type KeyDateChanged struct {
	MatterID  string `json:"matter_id"`
	KeyDateID string `json:"key_date_id"`
	UserID    string `json:"user_id"`
}

// PublishKeyDateChanged publishes EventKeyDateChanged in tx (nil: the emitter opens its own).
func PublishKeyDateChanged(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b KeyDateChanged) error {
	if b.MatterID == "" || b.KeyDateID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventKeyDateChanged, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "matter", ID: b.MatterID}, EventKeyDateChanged, b)
}

// TemplateApplied is the body of EventTemplateApplied: a matter template was applied.
type TemplateApplied struct {
	MatterID   string `json:"matter_id"`
	TemplateID string `json:"template_id"`
	UserID     string `json:"user_id"`
}

// PublishTemplateApplied publishes EventTemplateApplied in tx (nil: the emitter opens its own).
func PublishTemplateApplied(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b TemplateApplied) error {
	if b.MatterID == "" || b.TemplateID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventTemplateApplied, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "matter", ID: b.MatterID}, EventTemplateApplied, b)
}

// ItemAgentReady is the body of EventItemAgentReady: a work item became ready for the agent.
type ItemAgentReady struct {
	WorkItemID string `json:"work_item_id"`
	UserID     string `json:"user_id"`
}

// PublishItemAgentReady publishes EventItemAgentReady in tx (nil: the emitter opens its own).
func PublishItemAgentReady(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b ItemAgentReady) error {
	if b.WorkItemID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventItemAgentReady, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "work_item", ID: b.WorkItemID}, EventItemAgentReady, b)
}

// ItemAssignedToAgent is the body of EventItemAssignedToAgent: a work item was assigned to the agent.
type ItemAssignedToAgent struct {
	WorkItemID string `json:"work_item_id"`
	UserID     string `json:"user_id"`
}

// PublishItemAssignedToAgent publishes EventItemAssignedToAgent in tx (nil: the emitter opens its own).
func PublishItemAssignedToAgent(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b ItemAssignedToAgent) error {
	if b.WorkItemID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventItemAssignedToAgent, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "work_item", ID: b.WorkItemID}, EventItemAssignedToAgent, b)
}

// WorkflowDraftSaved is the body of EventWorkflowDraftSaved: a workflow draft was saved.
type WorkflowDraftSaved struct {
	WorkflowID string `json:"workflow_id"`
	UserID     string `json:"user_id"`
}

// PublishWorkflowDraftSaved publishes EventWorkflowDraftSaved in tx (nil: the emitter opens its own).
func PublishWorkflowDraftSaved(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b WorkflowDraftSaved) error {
	if b.WorkflowID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventWorkflowDraftSaved, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "workflow", ID: b.WorkflowID}, EventWorkflowDraftSaved, b)
}

// publishEvent emits one event; a nil emitter publishes nothing.
func publishEvent(ctx context.Context, em outbox.Emitter, tx pgx.Tx, s outbox.Subject, typ string, body any) error {
	if em == nil {
		return nil
	}
	// rule: ARC-I-030
	if err := em.Emit(ctx, tx, s, typ, body); err != nil {
		return fmt.Errorf("publish %s: %w", typ, err)
	}
	return nil
}
