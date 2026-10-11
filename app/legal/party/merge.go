package party

import (
	"context"
	"fmt"
	"strings"

	"github.com/dagi377/counsel/app/legal/intake"
)

// a4 merges intake.Contact and matters.Contact into one table, party.contact. An intake lead
// and the matter it converts into point at the same contact row: the lead creates it, and the
// matter's client_id is the lead's contact id. Neither intake nor matter keeps its own contact.

// FromSubmission maps the contact half of an intake submission to a party.contact create.
// Intake contacts are people; the name is used for both legal and display name until a
// lawyer edits it. Names are client data: the error never carries them.
func FromSubmission(s intake.Submission, createdBy string) (NewContact, error) {
	name := strings.TrimSpace(s.ContactName)
	if name == "" {
		return NewContact{}, fmt.Errorf("intake contact: empty name: %w", ErrInvalid)
	}
	return NewContact{
		Kind:              Person,
		LegalName:         name,
		DisplayName:       name,
		PreferredLanguage: "en",
		CreatedBy:         createdBy,
	}, nil
}

// CreateFromSubmission creates the party.contact for an intake submission.
//
// rule: ARC-I-040
// party owns party.contact: intake creates its contact through the party command API,
// never by writing the table itself.
func CreateFromSubmission(ctx context.Context, c Contacts, s intake.Submission, createdBy string) (Contact, error) {
	nc, err := FromSubmission(s, createdBy)
	if err != nil {
		return Contact{}, err
	}
	out, err := c.Create(ctx, nc)
	if err != nil {
		return Contact{}, fmt.Errorf("create intake contact: %w", err)
	}
	return out, nil
}

// ClientOfLead returns the contact a matter converted from lead must use as its client.
// Conversion keeps the lead's contact id; it never creates a second contact.
//
// rule: ARC-I-040
// matter reads the contact through the party query API to confirm it exists; it does not
// copy or rewrite the row.
func ClientOfLead(ctx context.Context, c Contacts, lead intake.Lead) (ContactID, error) {
	if lead.ContactID == "" {
		return "", fmt.Errorf("lead %s: no contact: %w", lead.ID, ErrInvalid)
	}
	got, err := c.Get(ctx, ContactID(lead.ContactID))
	if err != nil {
		return "", fmt.Errorf("lead %s contact: %w", lead.ID, err)
	}
	if got.State == Merged {
		return "", fmt.Errorf("lead %s contact is merged: %w", lead.ID, ErrInvalid)
	}
	return got.ID, nil
}
