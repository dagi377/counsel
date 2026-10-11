// Minimal publishers for the kernel events this package's agent features bind to but no
// write path publishes yet (EVENTS-9, docs/QUESTIONS.md Q-EVENTS-9). The owning write path
// calls its publisher in the same transaction when it lands. Bodies carry ids, days, counts
// and codes only, plus user_id: the user an event run acts for (trigger.PayloadActor).

package intake

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrEventBody means an event body lacks a required id; nothing is published.
var ErrEventBody = errors.New("intake: event body missing a required field")

// DictationUploaded is the body of EventDictationUploaded: a consultation dictation was uploaded.
type DictationUploaded struct {
	ConsultationID string `json:"consultation_id"`
	DocumentID     string `json:"document_id"`
	UserID         string `json:"user_id"`
}

// PublishDictationUploaded publishes EventDictationUploaded in tx (nil: the emitter opens its own).
func PublishDictationUploaded(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b DictationUploaded) error {
	if b.ConsultationID == "" || b.DocumentID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventDictationUploaded, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "consultation", ID: b.ConsultationID}, EventDictationUploaded, b)
}

// ConsultationEnded is the body of EventConsultationEnded: a consultation ended.
type ConsultationEnded struct {
	ConsultationID string `json:"consultation_id"`
	LeadID         string `json:"lead_id"`
	UserID         string `json:"user_id"`
}

// PublishConsultationEnded publishes EventConsultationEnded in tx (nil: the emitter opens its own).
func PublishConsultationEnded(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b ConsultationEnded) error {
	if b.ConsultationID == "" || b.LeadID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventConsultationEnded, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "consultation", ID: b.ConsultationID}, EventConsultationEnded, b)
}

// ConsultationRetained is the body of EventConsultRetained: a consultation ended with the client retaining the firm.
type ConsultationRetained struct {
	ConsultationID string `json:"consultation_id"`
	LeadID         string `json:"lead_id"`
	UserID         string `json:"user_id"`
}

// PublishConsultationRetained publishes EventConsultRetained in tx (nil: the emitter opens its own).
func PublishConsultationRetained(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b ConsultationRetained) error {
	if b.ConsultationID == "" || b.LeadID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventConsultRetained, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "consultation", ID: b.ConsultationID}, EventConsultRetained, b)
}

// LeadDeclinedEvent is the body of EventLeadDeclined: a lead was declined.
type LeadDeclinedEvent struct {
	LeadID     string `json:"lead_id"`
	ReasonCode string `json:"reason_code"`
	UserID     string `json:"user_id"`
}

// PublishLeadDeclinedEvent publishes EventLeadDeclined in tx (nil: the emitter opens its own).
func PublishLeadDeclinedEvent(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b LeadDeclinedEvent) error {
	if b.LeadID == "" || b.ReasonCode == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventLeadDeclined, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "lead", ID: b.LeadID}, EventLeadDeclined, b)
}

// LeadReferredEvent is the body of EventLeadReferred: a lead was referred out.
type LeadReferredEvent struct {
	LeadID     string `json:"lead_id"`
	ReasonCode string `json:"reason_code"`
	UserID     string `json:"user_id"`
}

// PublishLeadReferredEvent publishes EventLeadReferred in tx (nil: the emitter opens its own).
func PublishLeadReferredEvent(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b LeadReferredEvent) error {
	if b.LeadID == "" || b.ReasonCode == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventLeadReferred, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "lead", ID: b.LeadID}, EventLeadReferred, b)
}

// ReceptionistConversationStarted is the body of EventReceptionistConversationStarted: an inbound receptionist conversation started.
type ReceptionistConversationStarted struct {
	ConversationID string `json:"conversation_id"`
	Channel        string `json:"channel"`
	LeadID         string `json:"lead_id,omitempty"`
	UserID         string `json:"user_id"`
}

// PublishReceptionistConversationStarted publishes EventReceptionistConversationStarted in tx (nil: the emitter opens its own).
func PublishReceptionistConversationStarted(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b ReceptionistConversationStarted) error {
	if b.ConversationID == "" || b.Channel == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventReceptionistConversationStarted, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "receptionist_conversation", ID: b.ConversationID}, EventReceptionistConversationStarted, b)
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
