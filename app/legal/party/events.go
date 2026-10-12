package party

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// EventOrganizationClientCreated is published when a contact is created as an organization
// client. F-CON-08 (beneficial ownership mapping) starts on it.
const EventOrganizationClientCreated = "party.organization_client_created"

// SubjectContact is the outbox subject kind of a contact.
const SubjectContact = "party.contact"

// OrganizationClientCreated is the body of EventOrganizationClientCreated: ids only, never
// the organization's name.
type OrganizationClientCreated struct {
	ContactID string `json:"contact_id"`
	CreatedBy string `json:"created_by,omitempty"`
}

// publishCreated emits, in tx, EventContactCreated for every contact created by a user and
// EventOrganizationClientCreated when c is also an organization client. A contact with no
// creating user starts no run (contact.created acts for that user), so it publishes only the
// organization event; a store with no emitter publishes nothing.
func (s *Store) publishCreated(ctx context.Context, tx pgx.Tx, c Contact, createdBy string) error {
	if s.Events == nil {
		return nil
	}
	if createdBy != "" {
		// rule: ARC-I-030
		if err := PublishContactCreated(ctx, s.Events, tx, ContactCreated{
			ContactID: string(c.ID), Kind: string(c.Kind), UserID: createdBy,
		}); err != nil {
			return err
		}
	}
	if c.Kind != Organization || !c.IsClient {
		return nil
	}
	// rule: ARC-I-030
	body := OrganizationClientCreated{ContactID: string(c.ID), CreatedBy: createdBy}
	return s.Events.Emit(ctx, tx, outbox.Subject{Kind: SubjectContact, ID: string(c.ID)}, EventOrganizationClientCreated, body)
}
