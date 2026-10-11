package comms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/eventroute"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapCommItem is the capability to record communications.
const CapCommItem = "comms.comm_item"

// InboundWrites stores an inbound email; *Inbound implements it.
type InboundWrites interface {
	Receive(ctx context.Context, e InboundEmail) (string, error)
}

var _ InboundWrites = (*Inbound)(nil)

// InboundHTTP serves POST /emails/inbound (email.received), Q-EVENTS-WIRE-5. An inbound
// email is not on a matter until it is filed, so the check is firm-wide.
type InboundHTTP struct {
	eventroute.Route
	Inbound InboundWrites
}

func init() { add(registerInboundRoutes) }

// registerInboundRoutes mounts the inbound route; boot wiring supplies the dependencies and
// until it does it answers 503.
func registerInboundRoutes(r registry.Registry) error {
	r.Routes((&InboundHTTP{}).Routes)
	return nil
}

// Routes mounts the inbound route with its write declaration.
func (h *InboundHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// Storing a received email is not gate-owned.
	r.Method(http.MethodPost, "/emails/inbound", server.Declare(h.receive, EventEmailReceived))
}

type inboundBody struct {
	ConnectorID    string          `json:"connector_id,omitempty"`
	ExternalID     string          `json:"external_id,omitempty"`
	ThreadRef      string          `json:"thread_ref,omitempty"`
	Participants   json.RawMessage `json:"participants"`
	DisplaySubject string          `json:"display_subject,omitempty"`
	BodyRef        string          `json:"body_ref,omitempty"`
	OccurredAt     time.Time       `json:"occurred_at"`
}

type inboundView struct {
	CommItemID string `json:"comm_item_id,omitempty"`
	Duplicate  bool   `json:"duplicate"`
}

func (h *InboundHTTP) receive(w http.ResponseWriter, r *http.Request) {
	h.Serve(w, r, h.Inbound != nil, CapCommItem, func(w http.ResponseWriter, r *http.Request, _ registry.Principal) {
		var b inboundBody
		if err := eventroute.Decode(r, &b); err != nil {
			h.Invalid(w, r, "Body is not valid")
			return
		}
		id, err := h.Inbound.Receive(r.Context(), InboundEmail{ConnectorID: b.ConnectorID, ExternalID: b.ExternalID,
			ThreadRef: b.ThreadRef, Participants: b.Participants, DisplaySubject: b.DisplaySubject, BodyRef: b.BodyRef,
			OccurredAt: b.OccurredAt})
		switch {
		case errors.Is(err, ErrInvalid):
			h.Invalid(w, r, "Body is not valid")
		case err != nil:
			h.Internal(w, r)
		case id == "":
			eventroute.JSON(w, http.StatusOK, inboundView{Duplicate: true})
		default:
			eventroute.JSON(w, http.StatusCreated, inboundView{CommItemID: id})
		}
	})
}
