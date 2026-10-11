package intake

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/kernel/outbox"
)

// LeadStore keeps leads and their idempotency keys.
type LeadStore interface {
	// ByIdempotencyKey returns the lead first created for (formID, key), or ErrNotFound.
	ByIdempotencyKey(ctx context.Context, formID, key string) (Lead, error)
	// Insert stores l under (l.FormID, key) and returns the stored lead with its id. If a lead
	// already holds (l.FormID, key), Insert returns that lead instead of storing l.
	Insert(ctx context.Context, l Lead, key string) (Lead, error)
}

// LeadStatuses moves a lead between statuses.
type LeadStatuses interface {
	// SetStatus sets lead id's status to to and returns the status it had before. It returns
	// ErrNotFound for an unknown lead and ErrLeadClosed for a converted, declined or sealed one.
	SetStatus(ctx context.Context, id string, to LeadStatus) (LeadStatus, error)
}

// Leads creates leads from published form submissions.
type Leads struct {
	forms    FormStore
	leads    LeadStore
	contacts party.Contacts
	now      func() time.Time
	// Events receives EventLeadCreated and EventPortalSubmission for each new lead, and
	// EventLeadAwaitingInfo when a lead starts awaiting info. Boot sets it; nil publishes
	// nothing.
	Events outbox.Emitter
	// Statuses moves leads between statuses. Boot sets it; nil makes SetStatus fail.
	Statuses LeadStatuses
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
	// A concurrent submit with the same key returns the first lead, whose contact is not
	// ours: that submission already published.
	if l.ContactID == string(c.ID) {
		if err := s.publishSubmission(ctx, l); err != nil {
			return Lead{}, err
		}
	}
	return l, nil
}

// SetStatus moves lead id to status to. Moving into LeadAwaitingInfo from another status
// publishes EventLeadAwaitingInfo once; a lead already awaiting info publishes nothing more.
func (s *Leads) SetStatus(ctx context.Context, id string, to LeadStatus) error {
	if id == "" || (to != LeadOpen && to != LeadAwaitingInfo) {
		return fmt.Errorf("set lead status: %w", ErrInvalid)
	}
	if s.Statuses == nil {
		return fmt.Errorf("set lead status: no status store: %w", ErrInvalid)
	}
	from, err := s.Statuses.SetStatus(ctx, id, to)
	if err != nil {
		return fmt.Errorf("set lead status %s: %w", id, err)
	}
	if to != LeadAwaitingInfo || from == LeadAwaitingInfo {
		return nil
	}
	// rule: ARC-I-030
	if err := s.emit(ctx, id, EventLeadAwaitingInfo, AwaitingInfo{LeadID: id}); err != nil {
		return fmt.Errorf("set lead status %s: %w", id, err)
	}
	return nil
}
