package intake

import (
	"context"
	"fmt"

	"github.com/dagi377/counsel/kernel/outbox"
)

// EventPortalSubmission is published when a published intake form submitted through the
// portal creates a lead. F-CON-07 (ID evidence check) starts on it.
const EventPortalSubmission = "identity.portal_submission"

// SubjectLead is the outbox subject kind of a lead.
const SubjectLead = "intake.lead"

// PortalSubmission is the body of EventPortalSubmission: ids only, never the answers or the
// contact's name, email or phone.
type PortalSubmission struct {
	LeadID    string `json:"lead_id"`
	FormID    string `json:"form_id"`
	ContactID string `json:"contact_id"`
}

// publishSubmission emits EventPortalSubmission for lead l. Leads has no transaction of its
// own (LeadStore owns it), so the emitter gets a nil tx; boot wires one that opens its own.
func (s *Leads) publishSubmission(ctx context.Context, l Lead) error {
	if s.Events == nil {
		return nil
	}
	// rule: ARC-I-030
	body := PortalSubmission{LeadID: l.ID, FormID: l.FormID, ContactID: l.ContactID}
	if err := s.Events.Emit(ctx, nil, outbox.Subject{Kind: SubjectLead, ID: l.ID}, EventPortalSubmission, body); err != nil {
		return fmt.Errorf("submit: publish: %w", err)
	}
	return nil
}
