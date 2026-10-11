package party

import (
	"context"
	"fmt"
	"strings"
)

// a4 merges intake.Contact and matters.Contact into one table, party.contact. An intake lead
// and the matter it converts into point at the same contact row: the lead creates it, and the
// matter's client_id is the lead's contact id. Neither intake nor matter keeps its own contact.

// Submission is the contact half of an intake submission as party reads it.
// intake.Submission implements it. party sits below intake (intake creates its contacts
// through party), so party reads intake values through this interface and never imports
// intake.
type Submission interface {
	// SubmittedContactName is the contact name as typed on the form.
	SubmittedContactName() string
}

// Lead is an intake lead as party reads it. intake.Lead implements it.
type Lead interface {
	// LeadContact returns the lead id and the party.contact id the lead created.
	LeadContact() (leadID, contactID string)
}

// FromSubmission maps the contact half of an intake submission to a party.contact create.
// Intake contacts are people; the name is used for both legal and display name until a
// lawyer edits it. Names are client data: the error never carries them.
func FromSubmission(s Submission, createdBy string) (NewContact, error) {
	name := strings.TrimSpace(s.SubmittedContactName())
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
func CreateFromSubmission(ctx context.Context, c Contacts, s Submission, createdBy string) (Contact, error) {
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
func ClientOfLead(ctx context.Context, c Contacts, lead Lead) (ContactID, error) {
	leadID, contactID := lead.LeadContact()
	if contactID == "" {
		return "", fmt.Errorf("lead %s: no contact: %w", leadID, ErrInvalid)
	}
	got, err := c.Get(ctx, ContactID(contactID))
	if err != nil {
		return "", fmt.Errorf("lead %s contact: %w", leadID, err)
	}
	if got.State == Merged {
		return "", fmt.Errorf("lead %s contact is merged: %w", leadID, ErrInvalid)
	}
	return got.ID, nil
}
