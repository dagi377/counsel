package intake

import (
	"context"
	"fmt"
	"strings"

	"github.com/dagi377/counsel/app/legal/conflict"
)

// SealedRetentionClass is the retention class a declined lead keeps once it is reduced to a
// sealed prospect (verify: RC-LEAD period with the lawyer).
const SealedRetentionClass = "RC-LEAD"

// OutcomeChecker reports whether a lead's consultation has an outcome. *Consultations
// implements it.
type OutcomeChecker interface {
	// RequireOutcome returns ErrOutcomeRequired unless a consultation on the lead has an outcome.
	RequireOutcome(ctx context.Context, leadID string) error
}

// ConflictRuns lists the conflict check runs opened for a lead. A lead's check is opened with
// the lead id as the run's subject, so conflict.Checker implements it.
type ConflictRuns interface {
	// Runs lists every run for the subject, oldest first.
	Runs(ctx context.Context, subjectID string) ([]conflict.Run, error)
}

// MatterOpener opens the matter a converted lead becomes and returns its id. The wiring
// decides the number, short name and responsible lawyer.
type MatterOpener interface {
	OpenFromLead(ctx context.Context, l Lead, by string) (string, error)
}

// LeadLifecycle reads and closes leads.
type LeadLifecycle interface {
	// Lead returns one lead or ErrNotFound.
	Lead(ctx context.Context, id string) (Lead, error)
	// MarkConverted sets the lead converted to matterID and returns the updated lead.
	MarkConverted(ctx context.Context, leadID, matterID string) (Lead, error)
	// Seal reduces the lead to a sealed prospect kept under retentionClass: the status becomes
	// sealed_prospect and only what that class keeps survives. It returns the updated lead.
	Seal(ctx context.Context, leadID, retentionClass string) (Lead, error)
}

// Conversions turns leads into matters or seals them as prospects.
type Conversions struct {
	leads    LeadLifecycle
	outcomes OutcomeChecker
	runs     ConflictRuns
	matters  MatterOpener
}

// NewConversions returns a Conversions over the given ports.
func NewConversions(leads LeadLifecycle, outcomes OutcomeChecker, runs ConflictRuns, matters MatterOpener) *Conversions {
	return &Conversions{leads: leads, outcomes: outcomes, runs: runs, matters: matters}
}

// Convert turns an open lead into a matter. A consultation must have an outcome and the
// lead's conflict run (Lead.ConflictRef) must be cleared through gate G2. It returns the new
// matter id.
func (s *Conversions) Convert(ctx context.Context, leadID, by string) (string, error) {
	if leadID == "" || strings.TrimSpace(by) == "" {
		return "", fmt.Errorf("convert: %w", ErrInvalid)
	}
	l, err := s.openLead(ctx, leadID)
	if err != nil {
		return "", fmt.Errorf("convert: %w", err)
	}
	if err := s.outcomes.RequireOutcome(ctx, leadID); err != nil {
		return "", fmt.Errorf("convert: %w", err)
	}
	if err := s.requireCleared(ctx, l); err != nil {
		return "", fmt.Errorf("convert: %w", err)
	}
	matterID, err := s.matters.OpenFromLead(ctx, l, by)
	if err != nil {
		return "", fmt.Errorf("convert: open matter: %w", err)
	}
	if _, err := s.leads.MarkConverted(ctx, leadID, matterID); err != nil {
		return "", fmt.Errorf("convert: mark lead converted: %w", err)
	}
	return matterID, nil
}

// Decline closes an open lead and reduces it to a sealed prospect under RC-LEAD (verify).
func (s *Conversions) Decline(ctx context.Context, leadID, by string) (Lead, error) {
	if leadID == "" || strings.TrimSpace(by) == "" {
		return Lead{}, fmt.Errorf("decline: %w", ErrInvalid)
	}
	if _, err := s.openLead(ctx, leadID); err != nil {
		return Lead{}, fmt.Errorf("decline: %w", err)
	}
	l, err := s.leads.Seal(ctx, leadID, SealedRetentionClass)
	if err != nil {
		return Lead{}, fmt.Errorf("decline: seal lead: %w", err)
	}
	return l, nil
}

func (s *Conversions) openLead(ctx context.Context, leadID string) (Lead, error) {
	l, err := s.leads.Lead(ctx, leadID)
	if err != nil {
		return Lead{}, fmt.Errorf("lead %s: %w", leadID, err)
	}
	if l.Status != LeadOpen {
		return Lead{}, fmt.Errorf("lead %s is %s: %w", leadID, l.Status, ErrLeadClosed)
	}
	return l, nil
}

// requireCleared returns ErrConflictNotCleared unless the lead's conflict run is cleared.
func (s *Conversions) requireCleared(ctx context.Context, l Lead) error {
	if l.ConflictRef == "" {
		return fmt.Errorf("lead %s has no conflict check: %w", l.ID, ErrConflictNotCleared)
	}
	runs, err := s.runs.Runs(ctx, l.ID)
	if err != nil {
		return fmt.Errorf("conflict runs for lead %s: %w", l.ID, err)
	}
	for _, r := range runs {
		if r.ID != l.ConflictRef {
			continue
		}
		if r.Status != conflict.StatusCleared {
			return fmt.Errorf("conflict run %s is %s: %w", r.ID, r.Status, ErrConflictNotCleared)
		}
		return nil
	}
	return fmt.Errorf("conflict run %s not found for lead %s: %w", l.ConflictRef, l.ID, ErrConflictNotCleared)
}
