package timekeeping

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/outbox"
)

// Outbox subject kinds of the timekeeping events that are not about a matter.
const (
	SubjectTimesheet  = "timekeeping.timesheet"
	SubjectRateChange = "timekeeping.rate_change"
)

// BillDraftRequested is the body of EventBillDraftRequested: a user asked for a draft bill
// on a matter. Ids and the cut-off day only.
type BillDraftRequested struct {
	MatterID string `json:"matter_id"`
	UserID   string `json:"user_id"`
	PeriodTo string `json:"period_to,omitempty"` // YYYY-MM-DD, America/Toronto
}

// TimesheetWeekSubmitted is the body of EventTimesheetWeekSubmitted: a timekeeper submitted
// the week starting WeekStart (a Monday, YYYY-MM-DD) for review (G9c).
type TimesheetWeekSubmitted struct {
	UserID    string `json:"user_id"`
	WeekStart string `json:"week_start"`
}

// EngagementLetterSigned is the body of EventEngagementLetterSigned: the engagement letter
// document of a matter reached status signed. Ids only, never the letter text.
type EngagementLetterSigned struct {
	MatterID   string `json:"matter_id"`
	DocumentID string `json:"document_id"`
	UserID     string `json:"user_id"`
}

// RateChangeDrafted is the body of EventRateChangeDrafted: a rate change was drafted before
// its effective day. Ids and the day only; the rates stay in the rate tables.
type RateChangeDrafted struct {
	RateChangeID  string `json:"rate_change_id"`
	EffectiveFrom string `json:"effective_from"` // YYYY-MM-DD, America/Toronto
	UserID        string `json:"user_id"`
}

// PublishBillDraftRequested publishes EventBillDraftRequested in tx (nil: the emitter opens
// its own). Minimal publisher: main has no draft-request write point yet (Q-EVENTS-7).
func PublishBillDraftRequested(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b BillDraftRequested) error {
	if b.MatterID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventBillDraftRequested, ErrInvalid)
	}
	return publish(ctx, em, tx, outbox.Subject{Kind: matterkind.Subject, ID: b.MatterID}, EventBillDraftRequested, b)
}

// PublishTimesheetWeekSubmitted publishes EventTimesheetWeekSubmitted. Minimal publisher:
// main has no week submission yet (Q-EVENTS-7).
func PublishTimesheetWeekSubmitted(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b TimesheetWeekSubmitted) error {
	if b.UserID == "" || b.WeekStart == "" {
		return fmt.Errorf("publish %s: %w", EventTimesheetWeekSubmitted, ErrInvalid)
	}
	s := outbox.Subject{Kind: SubjectTimesheet, ID: b.UserID + ":" + b.WeekStart}
	return publish(ctx, em, tx, s, EventTimesheetWeekSubmitted, b)
}

// PublishEngagementLetterSigned publishes EventEngagementLetterSigned. Minimal publisher:
// main has no letter signing status yet (Q-EVENTS-7).
func PublishEngagementLetterSigned(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b EngagementLetterSigned) error {
	if b.MatterID == "" || b.DocumentID == "" {
		return fmt.Errorf("publish %s: %w", EventEngagementLetterSigned, ErrInvalid)
	}
	return publish(ctx, em, tx, outbox.Subject{Kind: matterkind.Subject, ID: b.MatterID}, EventEngagementLetterSigned, b)
}

// PublishRateChangeDrafted publishes EventRateChangeDrafted. Minimal publisher: main has no
// rate-change draft yet (Q-EVENTS-7).
func PublishRateChangeDrafted(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b RateChangeDrafted) error {
	if b.RateChangeID == "" || b.EffectiveFrom == "" {
		return fmt.Errorf("publish %s: %w", EventRateChangeDrafted, ErrInvalid)
	}
	return publish(ctx, em, tx, outbox.Subject{Kind: SubjectRateChange, ID: b.RateChangeID}, EventRateChangeDrafted, b)
}

// publish emits one event; a nil emitter publishes nothing.
func publish(ctx context.Context, em outbox.Emitter, tx pgx.Tx, s outbox.Subject, typ string, body any) error {
	if em == nil {
		return nil
	}
	// rule: ARC-I-030
	if err := em.Emit(ctx, tx, s, typ, body); err != nil {
		return fmt.Errorf("publish %s: %w", typ, err)
	}
	return nil
}
