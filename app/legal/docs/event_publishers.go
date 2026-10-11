// Minimal publishers for the kernel events this package's agent features bind to but no
// write path publishes yet (EVENTS-9, docs/QUESTIONS.md Q-EVENTS-9). The owning write path
// calls its publisher in the same transaction when it lands. Bodies carry ids, days, counts
// and codes only, plus user_id: the user an event run acts for (trigger.PayloadActor).

package docs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrEventBody means an event body lacks a required id; nothing is published.
var ErrEventBody = errors.New("docs: event body missing a required field")

// Kernel events published here that no feature file of this package declares.
const (
	EventDocumentReleased = "document.released" // a document version was released
)

// ConflictCleared is the body of EventConflictCleared: a conflict check was cleared.
type ConflictCleared struct {
	CheckID string `json:"check_id"`
	Outcome string `json:"outcome"`
	GateID  string `json:"gate_id"`
	UserID  string `json:"user_id"`
}

// PublishConflictCleared publishes EventConflictCleared in tx (nil: the emitter opens its own).
func PublishConflictCleared(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b ConflictCleared) error {
	if b.CheckID == "" || b.Outcome == "" || b.GateID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventConflictCleared, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "conflict_check", ID: b.CheckID}, EventConflictCleared, b)
}

// DraftCreated is the body of EventDraftCreated: a draft was created.
type DraftCreated struct {
	DraftID       string `json:"draft_id"`
	MatterID      string `json:"matter_id"`
	Origin        string `json:"origin"`
	AgentRunID    string `json:"agent_run_id,omitempty"`
	CitationCount int64  `json:"citation_count"`
	UserID        string `json:"user_id"`
}

// PublishDraftCreated publishes EventDraftCreated in tx (nil: the emitter opens its own).
func PublishDraftCreated(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b DraftCreated) error {
	if b.DraftID == "" || b.MatterID == "" || b.Origin == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventDraftCreated, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "draft", ID: b.DraftID}, EventDraftCreated, b)
}

// DocumentReleased is the body of EventDocumentReleased: a document version was released.
type DocumentReleased struct {
	DocumentID          string   `json:"document_id"`
	VersionID           string   `json:"version_id"`
	Channel             string   `json:"channel"`
	RecipientContactIDs []string `json:"recipient_contact_ids"`
	GateID              string   `json:"gate_id"`
	UserID              string   `json:"user_id"`
}

// PublishDocumentReleased publishes EventDocumentReleased in tx (nil: the emitter opens its own).
func PublishDocumentReleased(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b DocumentReleased) error {
	if b.DocumentID == "" || b.VersionID == "" || b.Channel == "" || b.GateID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventDocumentReleased, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "document", ID: b.DocumentID}, EventDocumentReleased, b)
}

// DocumentUploaded is the body of EventDocumentUploaded: a document version was uploaded.
type DocumentUploaded struct {
	DocumentID string `json:"document_id"`
	VersionID  string `json:"version_id"`
	MatterID   string `json:"matter_id"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes"`
	Mime       string `json:"mime"`
	UserID     string `json:"user_id"`
}

// PublishDocumentUploaded publishes EventDocumentUploaded in tx (nil: the emitter opens its own).
func PublishDocumentUploaded(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b DocumentUploaded) error {
	if b.DocumentID == "" || b.VersionID == "" || b.MatterID == "" || b.SHA256 == "" || b.Mime == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventDocumentUploaded, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "document", ID: b.DocumentID}, EventDocumentUploaded, b)
}

// LeadConverted is the body of EventLeadConverted: a lead became a matter.
type LeadConverted struct {
	LeadID   string `json:"lead_id"`
	MatterID string `json:"matter_id"`
	GateID   string `json:"gate_id"`
	UserID   string `json:"user_id"`
}

// PublishLeadConverted publishes EventLeadConverted in tx (nil: the emitter opens its own).
func PublishLeadConverted(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b LeadConverted) error {
	if b.LeadID == "" || b.MatterID == "" || b.GateID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventLeadConverted, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "lead", ID: b.LeadID}, EventLeadConverted, b)
}

// WorkItemCreated is the body of EventWorkItemCreated: a work item was created.
type WorkItemCreated struct {
	WorkItemID string `json:"work_item_id"`
	MatterID   string `json:"matter_id,omitempty"`
	AssigneeID string `json:"assignee_id,omitempty"`
	Origin     string `json:"origin"`
	UserID     string `json:"user_id"`
}

// PublishWorkItemCreated publishes EventWorkItemCreated in tx (nil: the emitter opens its own).
func PublishWorkItemCreated(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b WorkItemCreated) error {
	if b.WorkItemID == "" || b.Origin == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventWorkItemCreated, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "work_item", ID: b.WorkItemID}, EventWorkItemCreated, b)
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
