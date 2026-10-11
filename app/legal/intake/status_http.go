package intake

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/eventroute"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapLeads is the capability that works leads (the intake screens' capability).
const CapLeads = "intake.leads"

// CodeLeadClosed is the problem code for a converted, declined or sealed lead.
const CodeLeadClosed = "lead_closed"

// LeadStatusWrites moves a lead between statuses; *Leads implements it.
type LeadStatusWrites interface {
	SetStatus(ctx context.Context, id string, to LeadStatus) error
}

var _ LeadStatusWrites = (*Leads)(nil)

// The status route mounts unwired (503) until boot supplies its dependencies.
func init() {
	add(func(r registry.Registry) error {
		r.Routes((&StatusHTTP{}).Routes)
		return nil
	})
}

// StatusHTTP serves POST /leads/{lead_id}/awaiting-info, which sets a lead to awaiting info
// and publishes intake.lead_awaiting_info (Q-LEAD-STATUS). It needs intake.leads firm-wide.
type StatusHTTP struct {
	eventroute.Route
	Leads LeadStatusWrites
}

// Routes mounts the lead status route with its write declaration.
func (h *StatusHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// A lead awaiting info is not gate-owned: no money, trust or client communication moves.
	r.Method(http.MethodPost, "/leads/{lead_id}/awaiting-info", server.Declare(h.awaitingInfo, EventLeadAwaitingInfo))
}

type leadStatusView struct {
	LeadID string     `json:"lead_id"`
	Status LeadStatus `json:"status"`
}

func (h *StatusHTTP) awaitingInfo(w http.ResponseWriter, r *http.Request) {
	// rule: ARC-I-051
	h.Serve(w, r, h.Leads != nil, CapLeads, func(w http.ResponseWriter, r *http.Request, _ registry.Principal) {
		id := chi.URLParam(r, "lead_id")
		err := h.Leads.SetStatus(r.Context(), id, LeadAwaitingInfo)
		switch {
		case errors.Is(err, ErrNotFound):
			h.Fail(w, r, http.StatusNotFound, eventroute.CodeNotFound, "Not found")
		case errors.Is(err, ErrLeadClosed):
			h.Fail(w, r, http.StatusConflict, CodeLeadClosed, "Lead is closed")
		case errors.Is(err, ErrInvalid):
			h.Invalid(w, r, "Lead id is not valid")
		case err != nil:
			h.Internal(w, r)
		default:
			eventroute.JSON(w, http.StatusOK, leadStatusView{LeadID: id, Status: LeadAwaitingInfo})
		}
	})
}
