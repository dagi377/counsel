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

// publishCreated emits EventOrganizationClientCreated in tx when c is an organization client.
// A person, or an organization that is not a client, publishes nothing; so does a store with
// no emitter.
func (s *Store) publishCreated(ctx context.Context, tx pgx.Tx, c Contact, createdBy string) error {
	if s.Events == nil || c.Kind != Organization || !c.IsClient {
		return nil
	}
	// rule: ARC-I-030
	body := OrganizationClientCreated{ContactID: string(c.ID), CreatedBy: createdBy}
	return s.Events.Emit(ctx, tx, outbox.Subject{Kind: SubjectContact, ID: string(c.ID)}, EventOrganizationClientCreated, body)
}
