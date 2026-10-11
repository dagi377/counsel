// CONTRACT: app/legal/intake. Exported shapes here are frozen after wave 0; changes are
// additive only and need a docs/QUESTIONS.md entry. Bodies live in the T-033 subtasks.

// Package intake turns enquiries into matters: published forms create leads, leads book
// consultations, and a lead converts to a matter only after the conflict gate (G2) clears.
package intake

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned for an unknown form, lead or consultation.
	ErrNotFound = errors.New("intake: not found")
	// ErrFormNotPublished is returned when a submission targets a form that is not published.
	ErrFormNotPublished = errors.New("intake: form not published")
	// ErrInvalid is returned for a submission or booking with missing fields (422).
	ErrInvalid = errors.New("intake: invalid input")
	// ErrOutcomeRequired is returned when a lead converts before its consultation has an outcome.
	ErrOutcomeRequired = errors.New("intake: consultation outcome required")
	// ErrConflictNotCleared is returned when a lead converts before its conflict gate is approved.
	ErrConflictNotCleared = errors.New("intake: conflict gate not cleared")
	// ErrLeadClosed is returned when a converted or declined lead is changed.
	ErrLeadClosed = errors.New("intake: lead is closed")
)

// LeadStatus is a lead's state.
type LeadStatus string

// Lead statuses. A declined lead is reduced to a sealed prospect per its retention class.
const (
	LeadOpen           LeadStatus = "open"
	LeadAwaitingInfo   LeadStatus = "awaiting_info" // additive, Q-EVENTS-WIRE-6
	LeadConverted      LeadStatus = "converted"
	LeadDeclined       LeadStatus = "declined"
	LeadSealedProspect LeadStatus = "sealed_prospect"
)

// Outcome is how a consultation ended.
type Outcome string

// Outcomes. OutcomeNone means the consultation has not been held or recorded.
const (
	OutcomeNone     Outcome = ""
	OutcomeRetain   Outcome = "retain"
	OutcomeDecline  Outcome = "decline"
	OutcomeFollowUp Outcome = "follow_up"
)

// Submission is one filled-in published intake form.
type Submission struct {
	FormID         string
	ContactName    string
	ContactEmail   string
	ContactPhone   string
	Answers        map[string]string
	IdempotencyKey string
}

// Lead is a prospective client and the party.contact it created.
type Lead struct {
	ID          string
	FormID      string
	ContactID   string
	Status      LeadStatus
	CreatedAt   time.Time
	MatterID    string // set when converted
	ConflictRef string // the conflict gate id, set when a check is opened
}

// Consultation is a meeting booked against a lead.
type Consultation struct {
	ID       string
	LeadID   string
	StartsAt time.Time
	With     string // user id
	Outcome  Outcome
}

// Service is the intake surface other tasks call.
type Service interface {
	// Submit creates a lead and its contact from a published form; the same form and
	// idempotency key return the first lead.
	Submit(ctx context.Context, s Submission) (Lead, error)
	// Lead returns one lead or ErrNotFound.
	Lead(ctx context.Context, id string) (Lead, error)
	// Book books a consultation against an open lead.
	Book(ctx context.Context, leadID, with string, startsAt time.Time) (Consultation, error)
	// RecordOutcome sets a consultation's outcome.
	RecordOutcome(ctx context.Context, consultationID string, o Outcome) (Consultation, error)
	// Convert turns the lead into a matter once a consultation has an outcome and the
	// conflict gate (G2) is approved; it returns the new matter id.
	Convert(ctx context.Context, leadID, by string) (string, error)
	// Decline closes the lead and seals it as a prospect.
	Decline(ctx context.Context, leadID, by string) (Lead, error)
}
