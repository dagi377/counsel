package deadlines

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/eventroute"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CalendarWrites adds a calendar event; *CalendarEvents implements it.
type CalendarWrites interface {
	Add(ctx context.Context, e NewCalendarEvent) (string, error)
}

var _ CalendarWrites = (*CalendarEvents)(nil)

// CalendarHTTP serves POST /calendar-events (calendar_event.added), Q-EVENTS-WIRE-5. An
// event on a matter checks deadlines.edit on that matter: a walled matter answers 404, a
// missing capability 403.
type CalendarHTTP struct {
	eventroute.Route
	Calendar CalendarWrites
}

// Routes mounts the calendar route with its write declaration.
func (h *CalendarHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// Adding a calendar event is not gate-owned; deadlines are added by the engine.
	r.Method(http.MethodPost, "/calendar-events", server.Declare(h.add, EventCalendarEventAdded))
}

type calendarBody struct {
	CalendarID string    `json:"calendar_id"`
	Title      string    `json:"title"`
	Type       string    `json:"type"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	AllDay     bool      `json:"all_day,omitempty"`
	Location   string    `json:"location,omitempty"`
	MatterID   string    `json:"matter_id,omitempty"`
}

type calendarView struct {
	EventID  string `json:"event_id"`
	MatterID string `json:"matter_id,omitempty"`
}

func (h *CalendarHTTP) add(w http.ResponseWriter, r *http.Request) {
	h.Serve(w, r, h.Calendar != nil, CapEdit, func(w http.ResponseWriter, r *http.Request, p registry.Principal) {
		var b calendarBody
		if err := eventroute.Decode(r, &b); err != nil {
			h.Invalid(w, r, "Body is not valid")
			return
		}
		if b.MatterID != "" && !h.CanOnMatter(w, r, p, CapEdit, b.MatterID) {
			return
		}
		id, err := h.Calendar.Add(r.Context(), NewCalendarEvent{CalendarID: b.CalendarID, Title: b.Title, Type: b.Type,
			StartsAt: b.StartsAt, EndsAt: b.EndsAt, AllDay: b.AllDay, Location: b.Location, MatterID: b.MatterID})
		switch {
		case errors.Is(err, ErrInvalidEvent):
			h.Invalid(w, r, "Body is not valid")
		case err != nil:
			h.Internal(w, r)
		default:
			eventroute.JSON(w, http.StatusCreated, calendarView{EventID: id, MatterID: b.MatterID})
		}
	})
}
