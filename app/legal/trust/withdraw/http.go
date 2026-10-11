package withdraw

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapView reads withdrawals (spec/legal/access.json).
const CapView = "trust.view"

// Subject is the access subject kind of a withdrawal.
const Subject = "trust.withdrawal"

// Problem codes the withdrawal read routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
)

// Listed is a withdrawal with what its read view needs beyond Withdrawal: the client ledger
// it draws on and its row version.
type Listed struct {
	Withdrawal
	LedgerID string
	Version  int
}

// ListFilter selects one page of List: withdrawals whose id sorts after After, in Status
// when set, at most Limit.
type ListFilter struct {
	Status Status
	After  string
	Limit  int
}

// Lists is the withdrawal list query, read under the caller's RLS session. Service has no
// list; this port serves GET /trust/withdrawals and GET /trust/withdrawals/{withdrawal_id}
// (HTTP-11).
type Lists interface {
	// List returns one page of withdrawals, by id.
	List(ctx context.Context, f ListFilter) ([]Listed, error)
	// Listed returns one withdrawal or ErrNotFound for an unknown or hidden id.
	Listed(ctx context.Context, id string) (Listed, error)
}

// Access is the part of identity.Access the routes use.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// Reads serves GET /trust/withdrawals and GET /trust/withdrawals/{withdrawal_id}. Every
// dependency is required; a route answers 503 and calls nothing while one is missing.
type Reads struct {
	Lists    Lists
	Access   Access
	Cursors  api.CursorCodec
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// idPattern is the openapi Id format (uuid).
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Routes mounts the read routes. They write nothing. Authorizing a withdrawal is a decision
// on its trust.withdrawal gate, so the view carries the gate id and no authorize action.
func (h *Reads) Routes(r chi.Router) {
	r.Get("/trust/withdrawals", h.list)
	r.Get("/trust/withdrawals/{withdrawal_id}", h.get)
}

// apiStatus maps a withdrawal status to the openapi WithdrawalStatus; a status not listed
// passes through (the enum is extensible).
var apiStatus = map[Status]string{
	StatusRequested:  "awaiting_authorization",
	StatusAuthorized: "authorized",
	StatusExecuted:   "executed",
	StatusRejected:   "rejected",
	StatusCancelled:  "cancelled",
}

// statusOf is apiStatus reversed, for the status query parameter.
var statusOf = func() map[string]Status {
	out := map[string]Status{}
	for s, name := range apiStatus {
		out[name] = s
	}
	return out
}()

type actorView struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	ActingFor string `json:"acting_for,omitempty"`
}

// withdrawalView is the openapi WithdrawalRequest.
type withdrawalView struct {
	ID              string    `json:"id"`
	Version         int64     `json:"version"`
	LedgerID        string    `json:"ledger_id"`
	AmountCents     int64     `json:"amount_cents"`
	PurposeCategory string    `json:"purpose_category"`
	Payee           string    `json:"payee"`
	Method          string    `json:"method"`
	BillID          string    `json:"bill_id,omitempty"`
	Status          string    `json:"status"`
	GateID          string    `json:"gate_id,omitempty"`
	InitiatedBy     actorView `json:"initiated_by"`
	AuthorizedBy    []string  `json:"authorized_by,omitempty"`
	ExecutedBy      string    `json:"executed_by,omitempty"`
}

func withdrawalOf(l Listed) withdrawalView {
	st, ok := apiStatus[l.Status]
	if !ok {
		st = string(l.Status)
	}
	v := withdrawalView{ID: l.ID, Version: int64(l.Version), LedgerID: l.LedgerID, AmountCents: l.Request.AmountCents,
		PurposeCategory: l.Request.Reason, Payee: l.Request.Payee, Method: string(l.Request.Method), Status: st,
		GateID: l.GateID, InitiatedBy: actorView{Type: "user", ID: l.Request.PreparedBy}, ExecutedBy: l.ExecutedBy}
	if l.AuthorizedBy != "" {
		v.AuthorizedBy = []string{l.AuthorizedBy}
	}
	return v
}

func (h *Reads) ready() bool {
	return h.Lists != nil && h.Access != nil && h.Cursors != nil && h.Problems != nil && h.Principal != nil
}

func (h *Reads) problems() server.ProblemWriter {
	if h.Problems == nil {
		return &server.Problems{}
	}
	return h.Problems
}

func (h *Reads) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	h.problems().WriteProblem(w, r, server.NewProblem(status, code, title))
}

// writeErr maps an unexpected error to a problem. The error text never reaches the body.
func (h *Reads) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, api.ErrBadCursor) {
		h.problems().WriteProblem(w, r, api.BadCursorProblem())
		return
	}
	h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
}

// authorize answers 503, 401, 403 or 404 itself and returns false when the request cannot go
// on. It runs before any service call.
func (h *Reads) authorize(w http.ResponseWriter, r *http.Request, s registry.Subject) bool {
	if !h.ready() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return false
	}
	p, ok := h.Principal(r)
	if !ok {
		h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return false
	}
	// rule: ARC-I-051
	// Fail closed: an error or a missing capability stops the request.
	allowed, err := h.Access.Can(r.Context(), p, CapView, s)
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such withdrawal")
		return false
	case err != nil:
		h.writeErr(w, r, err)
		return false
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// list serves GET /trust/withdrawals: withdrawals by id, one page at a time.
func (h *Reads) list(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r, registry.Subject{}) {
		return
	}
	var f ListFilter
	if s := r.URL.Query().Get("status"); s != "" {
		st, ok := statusOf[s]
		if !ok {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "status is not valid")
			return
		}
		f.Status = st
	}
	cur, err := api.FromRequest(r, h.Cursors)
	if errors.Is(err, api.ErrBadLimit) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "limit must be an integer")
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	// One extra row tells whether another page follows.
	f.After, f.Limit = cur.After, cur.Limit+1
	got, err := h.Lists.List(r.Context(), f)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := api.Page[withdrawalView]{Items: []withdrawalView{}}
	for i, l := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: got[i-1].ID, Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.Next = next
			break
		}
		page.Items = append(page.Items, withdrawalOf(l))
	}
	writeJSON(w, http.StatusOK, page)
}

// get serves GET /trust/withdrawals/{withdrawal_id}. A malformed, unknown or hidden id
// answers 404.
func (h *Reads) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "withdrawal_id")
	if !idPattern.MatchString(id) {
		if h.authorize(w, r, registry.Subject{}) {
			h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such withdrawal")
		}
		return
	}
	if !h.authorize(w, r, registry.Subject{Kind: Subject, ID: id}) {
		return
	}
	l, err := h.Lists.Listed(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such withdrawal")
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, withdrawalOf(l))
}
