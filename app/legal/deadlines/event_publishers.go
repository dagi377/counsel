// Minimal publishers for the kernel events this package's agent features bind to but no
// write path publishes yet (EVENTS-9, docs/QUESTIONS.md Q-EVENTS-9). The owning write path
// calls its publisher in the same transaction when it lands. Bodies carry ids, days, counts
// and codes only, plus user_id: the user an event run acts for (trigger.PayloadActor).

package deadlines

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrEventBody means an event body lacks a required id; nothing is published.
var ErrEventBody = errors.New("deadlines: event body missing a required field")

// Kernel events published here that no feature file of this package declares.
const (
	EventDeadlineMet = "deadline.met" // a deadline was met
)

// CalendarEventCancelled is the body of EventCalendarEventCancelled: a calendar event was cancelled.
type CalendarEventCancelled struct {
	CalendarEventID string `json:"calendar_event_id"`
	MatterID        string `json:"matter_id,omitempty"`
	UserID          string `json:"user_id"`
}

// PublishCalendarEventCancelled publishes EventCalendarEventCancelled in tx (nil: the emitter opens its own).
func PublishCalendarEventCancelled(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b CalendarEventCancelled) error {
	if b.CalendarEventID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventCalendarEventCancelled, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "calendar_event", ID: b.CalendarEventID}, EventCalendarEventCancelled, b)
}

// CalendarEventEnded is the body of EventCalendarEventEnded: a calendar event ended.
type CalendarEventEnded struct {
	CalendarEventID string `json:"calendar_event_id"`
	MatterID        string `json:"matter_id,omitempty"`
	UserID          string `json:"user_id"`
}

// PublishCalendarEventEnded publishes EventCalendarEventEnded in tx (nil: the emitter opens its own).
func PublishCalendarEventEnded(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b CalendarEventEnded) error {
	if b.CalendarEventID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventCalendarEventEnded, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "calendar_event", ID: b.CalendarEventID}, EventCalendarEventEnded, b)
}

// CalendarEventMoved is the body of EventCalendarEventMoved: a calendar event moved to another day.
type CalendarEventMoved struct {
	CalendarEventID string `json:"calendar_event_id"`
	MatterID        string `json:"matter_id,omitempty"`
	FromDay         string `json:"from_day"`
	ToDay           string `json:"to_day"`
	UserID          string `json:"user_id"`
}

// PublishCalendarEventMoved publishes EventCalendarEventMoved in tx (nil: the emitter opens its own).
func PublishCalendarEventMoved(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b CalendarEventMoved) error {
	if b.CalendarEventID == "" || b.FromDay == "" || b.ToDay == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventCalendarEventMoved, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "calendar_event", ID: b.CalendarEventID}, EventCalendarEventMoved, b)
}

// AbsenceApproved is the body of EventAbsenceApproved: an absence was approved.
type AbsenceApproved struct {
	AbsenceID    string `json:"absence_id"`
	AbsentUserID string `json:"absent_user_id"`
	From         string `json:"from"`
	To           string `json:"to"`
	UserID       string `json:"user_id"`
}

// PublishAbsenceApproved publishes EventAbsenceApproved in tx (nil: the emitter opens its own).
func PublishAbsenceApproved(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b AbsenceApproved) error {
	if b.AbsenceID == "" || b.AbsentUserID == "" || b.From == "" || b.To == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventAbsenceApproved, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "absence", ID: b.AbsenceID}, EventAbsenceApproved, b)
}

// DeadlineMet is the body of EventDeadlineMet: a deadline was met.
type DeadlineMet struct {
	DeadlineID string `json:"deadline_id"`
	GateID     string `json:"gate_id,omitempty"`
	UserID     string `json:"user_id"`
}

// PublishDeadlineMet publishes EventDeadlineMet in tx (nil: the emitter opens its own).
func PublishDeadlineMet(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b DeadlineMet) error {
	if b.DeadlineID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventDeadlineMet, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "deadline", ID: b.DeadlineID}, EventDeadlineMet, b)
}

// publishEvent emits one event; a nil emitter publishes nothing.
func publishEvent(ctx context.Context, em outbox.Emitter, tx pgx.Tx, s outbox.Subject, typ string, body any) error {
	if em == nil {
		return nil
	}
	// rule: ARC-I-030
	if err := em.Emit(ctx, tx, s, typ, body); err != nil {
		return fmt.Errorf("publish %s: %w", typ, err)
	}
	return nil
}
