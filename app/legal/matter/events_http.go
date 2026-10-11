package matter

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/eventroute"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// EventWrites are the publishing matter write points the event routes call; *Store
// implements them.
type EventWrites interface {
	ChangeStage(ctx context.Context, c StageChange, gateID string) error
	AddNote(ctx context.Context, n NewNote) (string, error)
}

var _ EventWrites = (*Store)(nil)

// EventHTTP serves POST /matters/{matter_id}/stage (matter.stage_changed) and
// POST /matters/{matter_id}/notes (matter.note_added), Q-EVENTS-WIRE-5. Both check
// matters.edit on the matter: a walled matter answers 404, a missing capability 403.
type EventHTTP struct {
	eventroute.Route
	Writes EventWrites
	// Now is the stage change time.
	Now func() time.Time
}

func init() { add(registerEventRoutes) }

// registerEventRoutes mounts the event routes; boot wiring supplies the dependencies and
// until it does they answer 503.
func registerEventRoutes(r registry.Registry) error {
	r.Routes((&EventHTTP{}).Routes)
	return nil
}

// Routes mounts the event routes with their write declarations.
func (h *EventHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// Neither a stage change by a human nor a note is gate-owned here.
	r.Method(http.MethodPost, "/matters/{matter_id}/stage", server.Declare(h.stage, EventStageChanged))
	r.Method(http.MethodPost, "/matters/{matter_id}/notes", server.Declare(h.note, EventNoteAdded))
}

func (h *EventHTTP) ready() bool { return h.Writes != nil && h.Now != nil }

type stageBody struct {
	FromStageID string `json:"from_stage_id"`
	ToStageID   string `json:"to_stage_id"`
}

type stageView struct {
	MatterID  string `json:"matter_id"`
	ToStageID string `json:"to_stage_id"`
}

func (h *EventHTTP) stage(w http.ResponseWriter, r *http.Request) {
	h.Serve(w, r, h.ready(), CapEdit, func(w http.ResponseWriter, r *http.Request, p registry.Principal) {
		// rule: ARC-I-017
		// approved_by on the stage change is a person: an agent run may not move a stage.
		if p.Kind != registry.PrincipalUser {
			h.Fail(w, r, http.StatusForbidden, eventroute.CodeHumanOnly, "A person must change the stage")
			return
		}
		var b stageBody
		if err := eventroute.Decode(r, &b); err != nil || strings.TrimSpace(b.ToStageID) == "" {
			h.Invalid(w, r, "Body is not valid")
			return
		}
		id := chi.URLParam(r, "matter_id")
		if !h.CanOnMatter(w, r, p, CapEdit, id) {
			return
		}
		c := StageChange{MatterID: ID(id), FromStageID: b.FromStageID, ToStageID: b.ToStageID, ApprovedBy: p.UserID, At: h.Now()}
		if err := h.Writes.ChangeStage(r.Context(), c, ""); err != nil {
			h.writeErr(w, r, err)
			return
		}
		eventroute.JSON(w, http.StatusOK, stageView{MatterID: id, ToStageID: b.ToStageID})
	})
}

type noteBody struct {
	BodyRef    string `json:"body_ref"`
	Visibility string `json:"visibility,omitempty"`
	Privileged bool   `json:"privileged,omitempty"`
}

type noteView struct {
	NoteID   string `json:"note_id"`
	MatterID string `json:"matter_id"`
}

func (h *EventHTTP) note(w http.ResponseWriter, r *http.Request) {
	h.Serve(w, r, h.ready(), CapEdit, func(w http.ResponseWriter, r *http.Request, p registry.Principal) {
		var b noteBody
		if err := eventroute.Decode(r, &b); err != nil || strings.TrimSpace(b.BodyRef) == "" {
			h.Invalid(w, r, "Body is not valid")
			return
		}
		id := chi.URLParam(r, "matter_id")
		if !h.CanOnMatter(w, r, p, CapEdit, id) {
			return
		}
		noteID, err := h.Writes.AddNote(r.Context(), NewNote{MatterID: ID(id), AuthorID: eventroute.User(p),
			BodyRef: b.BodyRef, Visibility: b.Visibility, Privileged: b.Privileged})
		if err != nil {
			h.writeErr(w, r, err)
			return
		}
		eventroute.JSON(w, http.StatusCreated, noteView{NoteID: noteID, MatterID: id})
	})
}

// writeErr maps a write point error: a missing matter or stage 404, a bad field 400.
func (h *EventHTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		h.Fail(w, r, http.StatusNotFound, eventroute.CodeNotFound, "Not found")
	case errors.Is(err, ErrInvalid):
		h.Invalid(w, r, "Body is not valid")
	default:
		h.Internal(w, r)
	}
}
