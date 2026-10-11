// Package intakefake is an in-memory intake.Service for tests in other packages.
package intakefake

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/intake"
)

var _ intake.Service = (*Fake)(nil)

// Fake keeps forms, leads and consultations in memory.
type Fake struct {
	mu       sync.Mutex
	now      func() time.Time
	forms    map[string]bool // id -> published
	leads    map[string]*intake.Lead
	consults map[string]*intake.Consultation
	cleared  map[string]bool // lead id -> conflict gate approved
	dedup    map[string]string
	seq      int
}

// New returns an empty Fake that stamps leads with now().
func New(now func() time.Time) *Fake {
	return &Fake{now: now, forms: map[string]bool{}, leads: map[string]*intake.Lead{}, consults: map[string]*intake.Consultation{}, cleared: map[string]bool{}, dedup: map[string]string{}}
}

// AddForm adds a form, published or not.
func (f *Fake) AddForm(id string, published bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forms[id] = published
}

// ClearConflict marks the lead's conflict gate approved.
func (f *Fake) ClearConflict(leadID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared[leadID] = true
}

func (f *Fake) next(prefix string) string {
	f.seq++
	return prefix + strconv.Itoa(f.seq)
}

// Submit creates a lead from a published form.
func (f *Fake) Submit(_ context.Context, s intake.Submission) (intake.Lead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := s.FormID + "\x00" + s.IdempotencyKey
	if id, ok := f.dedup[key]; ok && s.IdempotencyKey != "" {
		return *f.leads[id], nil
	}
	published, ok := f.forms[s.FormID]
	if !ok {
		return intake.Lead{}, fmt.Errorf("intakefake: form %s: %w", s.FormID, intake.ErrNotFound)
	}
	if !published {
		return intake.Lead{}, intake.ErrFormNotPublished
	}
	if s.ContactName == "" || (s.ContactEmail == "" && s.ContactPhone == "") {
		return intake.Lead{}, intake.ErrInvalid
	}
	l := &intake.Lead{ID: f.next("lead_"), FormID: s.FormID, ContactID: f.next("contact_"), Status: intake.LeadOpen, CreatedAt: f.now()}
	f.leads[l.ID] = l
	if s.IdempotencyKey != "" {
		f.dedup[key] = l.ID
	}
	return *l, nil
}

func (f *Fake) lead(id string) (*intake.Lead, error) {
	l, ok := f.leads[id]
	if !ok {
		return nil, fmt.Errorf("intakefake: lead %s: %w", id, intake.ErrNotFound)
	}
	return l, nil
}

// Lead returns one lead.
func (f *Fake) Lead(_ context.Context, id string) (intake.Lead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, err := f.lead(id)
	if err != nil {
		return intake.Lead{}, err
	}
	return *l, nil
}

// Book books a consultation against an open lead.
func (f *Fake) Book(_ context.Context, leadID, with string, startsAt time.Time) (intake.Consultation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, err := f.lead(leadID)
	if err != nil {
		return intake.Consultation{}, err
	}
	if l.Status != intake.LeadOpen {
		return intake.Consultation{}, intake.ErrLeadClosed
	}
	if with == "" || startsAt.IsZero() {
		return intake.Consultation{}, intake.ErrInvalid
	}
	c := &intake.Consultation{ID: f.next("consult_"), LeadID: leadID, StartsAt: startsAt, With: with}
	f.consults[c.ID] = c
	return *c, nil
}

// RecordOutcome sets a consultation's outcome.
func (f *Fake) RecordOutcome(_ context.Context, id string, o intake.Outcome) (intake.Consultation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.consults[id]
	if !ok {
		return intake.Consultation{}, fmt.Errorf("intakefake: consultation %s: %w", id, intake.ErrNotFound)
	}
	if o == intake.OutcomeNone {
		return intake.Consultation{}, intake.ErrInvalid
	}
	c.Outcome = o
	return *c, nil
}

// Convert turns an open lead with a consultation outcome and a cleared conflict into a matter.
func (f *Fake) Convert(_ context.Context, leadID, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, err := f.lead(leadID)
	if err != nil {
		return "", err
	}
	if l.Status != intake.LeadOpen {
		return "", intake.ErrLeadClosed
	}
	held := false
	for _, c := range f.consults {
		if c.LeadID == leadID && c.Outcome != intake.OutcomeNone {
			held = true
		}
	}
	if !held {
		return "", intake.ErrOutcomeRequired
	}
	if !f.cleared[leadID] {
		return "", intake.ErrConflictNotCleared
	}
	l.Status, l.MatterID = intake.LeadConverted, f.next("matter_")
	return l.MatterID, nil
}

// Decline closes an open lead as a sealed prospect.
func (f *Fake) Decline(_ context.Context, leadID, _ string) (intake.Lead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, err := f.lead(leadID)
	if err != nil {
		return intake.Lead{}, err
	}
	if l.Status != intake.LeadOpen {
		return intake.Lead{}, intake.ErrLeadClosed
	}
	l.Status = intake.LeadSealedProspect
	return *l, nil
}
