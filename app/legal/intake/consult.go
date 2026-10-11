package intake

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// LeadReader reads one lead.
type LeadReader interface {
	// Lead returns one lead or ErrNotFound.
	Lead(ctx context.Context, id string) (Lead, error)
}

// ConsultationStore keeps consultations.
type ConsultationStore interface {
	// InsertConsultation stores c and returns it with its id.
	InsertConsultation(ctx context.Context, c Consultation) (Consultation, error)
	// Consultation returns one consultation or ErrNotFound.
	Consultation(ctx context.Context, id string) (Consultation, error)
	// SetOutcome stores the outcome of consultation id and returns the updated consultation.
	SetOutcome(ctx context.Context, id string, o Outcome) (Consultation, error)
	// ByLead returns the consultations booked against leadID, in booking order.
	ByLead(ctx context.Context, leadID string) ([]Consultation, error)
}

// Consultations books consultations against leads and records their outcomes.
type Consultations struct {
	leads    LeadReader
	consults ConsultationStore
}

// NewConsultations returns a Consultations over the given stores.
func NewConsultations(leads LeadReader, consults ConsultationStore) *Consultations {
	return &Consultations{leads: leads, consults: consults}
}

// Book books a consultation with user `with` at startsAt against an open lead.
func (s *Consultations) Book(ctx context.Context, leadID, with string, startsAt time.Time) (Consultation, error) {
	if leadID == "" || strings.TrimSpace(with) == "" || startsAt.IsZero() {
		return Consultation{}, fmt.Errorf("book: %w", ErrInvalid)
	}
	if err := s.requireOpen(ctx, leadID); err != nil {
		return Consultation{}, fmt.Errorf("book: %w", err)
	}
	c, err := s.consults.InsertConsultation(ctx, Consultation{LeadID: leadID, StartsAt: startsAt, With: with})
	if err != nil {
		return Consultation{}, fmt.Errorf("book: insert consultation: %w", err)
	}
	return c, nil
}

// RecordOutcome sets a consultation's outcome. The lead must still be open, and the outcome
// must be one of retain, decline or follow_up.
func (s *Consultations) RecordOutcome(ctx context.Context, consultationID string, o Outcome) (Consultation, error) {
	switch o {
	case OutcomeRetain, OutcomeDecline, OutcomeFollowUp:
	default:
		return Consultation{}, fmt.Errorf("record outcome %q: %w", o, ErrInvalid)
	}
	c, err := s.consults.Consultation(ctx, consultationID)
	if err != nil {
		return Consultation{}, fmt.Errorf("record outcome: consultation %s: %w", consultationID, err)
	}
	if err := s.requireOpen(ctx, c.LeadID); err != nil {
		return Consultation{}, fmt.Errorf("record outcome: %w", err)
	}
	c, err = s.consults.SetOutcome(ctx, consultationID, o)
	if err != nil {
		return Consultation{}, fmt.Errorf("record outcome: store: %w", err)
	}
	return c, nil
}

// RequireOutcome returns nil when at least one consultation booked against the lead has an
// outcome, and ErrOutcomeRequired otherwise. Conversion calls it before the conflict gate.
func (s *Consultations) RequireOutcome(ctx context.Context, leadID string) error {
	cs, err := s.consults.ByLead(ctx, leadID)
	if err != nil {
		return fmt.Errorf("require outcome: lead %s: %w", leadID, err)
	}
	for _, c := range cs {
		if c.Outcome != OutcomeNone {
			return nil
		}
	}
	return fmt.Errorf("require outcome: lead %s: %w", leadID, ErrOutcomeRequired)
}

func (s *Consultations) requireOpen(ctx context.Context, leadID string) error {
	l, err := s.leads.Lead(ctx, leadID)
	if err != nil {
		return fmt.Errorf("lead %s: %w", leadID, err)
	}
	if l.Status != LeadOpen {
		return fmt.Errorf("lead %s is %s: %w", leadID, l.Status, ErrLeadClosed)
	}
	return nil
}
