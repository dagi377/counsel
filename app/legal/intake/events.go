package intake

import (
	"context"
	"fmt"

	"github.com/dagi377/counsel/kernel/outbox"
)

// EventPortalSubmission is published when a published intake form submitted through the
// portal creates a lead. F-CON-07 (ID evidence check) starts on it.
const EventPortalSubmission = "identity.portal_submission"

// EventLeadCreated is published once for every new lead, a portal submission included
// (spec/eng/a2-interfaces.json). F-INT-01, F-CON-01 and F-DL-02 start on it.
const EventLeadCreated = "lead.created"

// EventLeadAwaitingInfo is published when a lead moves into LeadAwaitingInfo. The intake
// missing-info chaser starts on it (additive, docs/QUESTIONS.md Q-EVENTS-WIRE-6).
const EventLeadAwaitingInfo = "intake.lead_awaiting_info"

// SubjectLead is the outbox subject kind of a lead.
const SubjectLead = "intake.lead"

// PortalSubmission is the body of EventPortalSubmission: ids only, never the answers or the
// contact's name, email or phone.
type PortalSubmission struct {
	LeadID    string `json:"lead_id"`
	FormID    string `json:"form_id"`
	ContactID string `json:"contact_id"`
}

// LeadCreated is the body of EventLeadCreated: ids only. SourceID is the lead source, when
// the lead has one.
type LeadCreated struct {
	LeadID   string `json:"lead_id"`
	SourceID string `json:"source_id,omitempty"`
}

// AwaitingInfo is the body of EventLeadAwaitingInfo: ids only.
type AwaitingInfo struct {
	LeadID string `json:"lead_id"`
}

// emit publishes one lead event. Leads has no transaction of its own (LeadStore owns it), so
// the emitter gets a nil tx; boot wires one that opens its own.
func (s *Leads) emit(ctx context.Context, leadID, typ string, body any) error {
	if s.Events == nil {
		return nil
	}
	if err := s.Events.Emit(ctx, nil, outbox.Subject{Kind: SubjectLead, ID: leadID}, typ, body); err != nil {
		return fmt.Errorf("publish %s: %w", typ, err)
	}
	return nil
}

// publishSubmission emits EventLeadCreated and EventPortalSubmission for the new lead l.
func (s *Leads) publishSubmission(ctx context.Context, l Lead) error {
	// rule: ARC-I-030
	if err := s.emit(ctx, l.ID, EventLeadCreated, LeadCreated{LeadID: l.ID}); err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	// rule: ARC-I-030
	body := PortalSubmission{LeadID: l.ID, FormID: l.FormID, ContactID: l.ContactID}
	if err := s.emit(ctx, l.ID, EventPortalSubmission, body); err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	return nil
}
