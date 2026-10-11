package deadlines

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/outbox"
)

// EventCalendarEventAdded is published when an event is added to a calendar
// (spec/eng/a2-interfaces.json, Q-EVENTS-WIRE-4).
const EventCalendarEventAdded = "calendar_event.added"

// SubjectCalendarEvent is the outbox subject kind of a calendar event.
const SubjectCalendarEvent = "cal.calendar_event"

// ErrInvalidEvent means a calendar event is missing a field or is not well formed.
var ErrInvalidEvent = errors.New("deadlines: invalid calendar event")

// ErrNoWritePoint means the calendar store has no database or no emitter, so the event would
// be lost; it refuses instead of adding the calendar event without it.
var ErrNoWritePoint = errors.New("deadlines: write point not wired")

// NewCalendarEvent is an event added to a calendar. Deadline-type events are added by the
// deadline engine, not here.
type NewCalendarEvent struct {
	CalendarID string
	Title      string
	Type       string // meeting, court, consult, task or other
	StartsAt   time.Time
	EndsAt     time.Time
	AllDay     bool
	Location   string
	MatterID   string // empty when the event is not on a matter
}

// CalendarEventAdded is the body of EventCalendarEventAdded: never the title or location.
type CalendarEventAdded struct {
	EventID    string    `json:"event_id"`
	CalendarID string    `json:"calendar_id"`
	MatterID   string    `json:"matter_id,omitempty"`
	Type       string    `json:"type"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
}

// CalendarEvents adds calendar events.
type CalendarEvents struct {
	DB     db.DB
	Events outbox.Emitter
}

const addCalendarEventSQL = `INSERT INTO cal.calendar_event
  (calendar_id, title, type, starts_at, ends_at, all_day, location, matter_id)
VALUES ($1::uuid, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, '')::uuid)
RETURNING id::text`

// Add stores e and publishes EventCalendarEventAdded in the same tx. It returns the event id.
func (c *CalendarEvents) Add(ctx context.Context, e NewCalendarEvent) (string, error) {
	switch e.Type {
	case "meeting", "court", "consult", "task", "other":
	default:
		return "", fmt.Errorf("add calendar event: type %q: %w", e.Type, ErrInvalidEvent)
	}
	if e.CalendarID == "" || e.Title == "" || e.StartsAt.IsZero() || e.EndsAt.Before(e.StartsAt) {
		return "", fmt.Errorf("add calendar event: %w", ErrInvalidEvent)
	}
	if c.DB == nil || c.Events == nil {
		return "", fmt.Errorf("add calendar event: %w", ErrNoWritePoint)
	}
	var id string
	err := c.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, addCalendarEventSQL, e.CalendarID, e.Title, e.Type, e.StartsAt, e.EndsAt,
			e.AllDay, e.Location, e.MatterID).Scan(&id)
		if err != nil {
			return err
		}
		// rule: ARC-I-030
		body := CalendarEventAdded{EventID: id, CalendarID: e.CalendarID, MatterID: e.MatterID, Type: e.Type, StartsAt: e.StartsAt, EndsAt: e.EndsAt}
		return c.Events.Emit(ctx, tx, outbox.Subject{Kind: SubjectCalendarEvent, ID: id}, EventCalendarEventAdded, body)
	})
	if err != nil {
		return "", fmt.Errorf("add calendar event: %w", err)
	}
	return id, nil
}
