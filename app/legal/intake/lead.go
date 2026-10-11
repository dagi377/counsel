package intake

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dagi377/counsel/app/legal/party"
)

// LeadStore keeps leads and their idempotency keys.
type LeadStore interface {
	// ByIdempotencyKey returns the lead first created for (formID, key), or ErrNotFound.
	ByIdempotencyKey(ctx context.Context, formID, key string) (Lead, error)
	// Insert stores l under (l.FormID, key) and returns the stored lead with its id. If a lead
	// already holds (l.FormID, key), Insert returns that lead instead of storing l.
	Insert(ctx context.Context, l Lead, key string) (Lead, error)
}

// Leads creates leads from published form submissions.
type Leads struct {
	forms    FormStore
	leads    LeadStore
	contacts party.Contacts
	now      func() time.Time
}

// NewLeads returns a Leads that stamps new leads with now().
func NewLeads(forms FormStore, leads LeadStore, contacts party.Contacts, now func() time.Time) *Leads {
	return &Leads{forms: forms, leads: leads, contacts: contacts, now: now}
}

// Submit creates a lead and its party.contact from a published form. The same form and
// idempotency key return the first lead and create nothing new.
func (s *Leads) Submit(ctx context.Context, sub Submission) (Lead, error) {
	if sub.FormID == "" || strings.TrimSpace(sub.ContactName) == "" ||
		(sub.ContactEmail == "" && sub.ContactPhone == "") {
		return Lead{}, fmt.Errorf("submit: %w", ErrInvalid)
	}
	form, err := s.forms.Form(ctx, sub.FormID)
	if err != nil {
		return Lead{}, fmt.Errorf("submit: form %s: %w", sub.FormID, err)
	}
	if !form.Published {
		return Lead{}, fmt.Errorf("submit: form %s: %w", sub.FormID, ErrFormNotPublished)
	}
	if sub.IdempotencyKey != "" {
		l, err := s.leads.ByIdempotencyKey(ctx, sub.FormID, sub.IdempotencyKey)
		switch {
		case err == nil:
			return l, nil
		case !errors.Is(err, ErrNotFound):
			return Lead{}, fmt.Errorf("submit: idempotency lookup: %w", err)
		}
	}
	name := strings.TrimSpace(sub.ContactName)
	c, err := s.contacts.Create(ctx, party.NewContact{Kind: party.Person, LegalName: name, DisplayName: name})
	if err != nil {
		return Lead{}, fmt.Errorf("submit: create contact: %w", err)
	}
	l, err := s.leads.Insert(ctx, Lead{
		FormID:    sub.FormID,
		ContactID: string(c.ID),
		Status:    LeadOpen,
		CreatedAt: s.now(),
	}, sub.IdempotencyKey)
	if err != nil {
		return Lead{}, fmt.Errorf("submit: insert lead: %w", err)
	}
	return l, nil
}
