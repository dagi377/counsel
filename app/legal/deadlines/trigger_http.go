package deadlines

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapEdit is the capability to change a stored deadline.
const CapEdit = "deadlines.edit"

// Problem codes of the trigger route.
const (
	CodeUnavailable       = "unavailable"
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
)

// TriggerChanger changes a stored deadline's trigger date in tx; *deadline.PGBook
// implements it and publishes deadline.trigger_date_changed.
type TriggerChanger interface {
	// Get returns the stored deadline; its subject is the matter whose wall and capability
	// the route checks.
	Get(ctx context.Context, id deadline.DeadlineID) (deadline.Deadline, error)
	ChangeTrigger(ctx context.Context, tx pgx.Tx, id deadline.DeadlineID, trigger deadline.Date) error
}

// TxRunner runs fn in one transaction; kernel/db implements it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// Access is identity.Access as the route uses it.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

var _ TriggerChanger = (*deadline.PGBook)(nil)

// TriggerHTTP serves POST /deadlines/{deadline_id}/trigger (Q-EVENTS-WIRE-2): it sets the
// trigger date and publishes deadline.trigger_date_changed; the due dates are recomputed and
// proposed by F-DL-03, never changed here. It answers 503 while a dependency is missing.
type TriggerHTTP struct {
	Book     TriggerChanger
	DB       TxRunner
	Access   Access
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// Routes mounts the trigger route with its write declaration.
func (h *TriggerHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// The trigger date is not gate-owned; confirming the recomputed due dates is.
	r.Method(http.MethodPost, "/deadlines/{deadline_id}/trigger", server.Declare(h.change, deadline.EventTriggerDateChanged))
}

func (h *TriggerHTTP) ready() bool {
	return h.Book != nil && h.DB != nil && h.Access != nil && h.Idem != nil && h.Problems != nil && h.Principal != nil
}

func (h *TriggerHTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	pw := h.Problems
	if pw == nil {
		pw = &server.Problems{}
	}
	pw.WriteProblem(w, r, server.NewProblem(status, code, title))
}

type triggerBody struct {
	TriggerOn string `json:"trigger_on"`
}

type triggerView struct {
	DeadlineID string `json:"deadline_id"`
	TriggerOn  string `json:"trigger_on"`
}

// change serves POST /deadlines/{deadline_id}/trigger.
func (h *TriggerHTTP) change(w http.ResponseWriter, r *http.Request) {
	if !h.ready() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return
	}
	p, ok := h.Principal(r)
	if !ok {
		h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return
	}
	// rule: ARC-I-051
	// Fail closed: an error or a missing capability stops the request.
	allowed, err := h.Access.Can(r.Context(), p, CapEdit, registry.Subject{})
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
		return
	case err != nil:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return
	}
	// rule: ARC-I-007
	// The key is required before any store call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return
	}
	user := p.UserID
	if p.Kind == registry.PrincipalAgent {
		user = p.ActingFor
	}
	// rule: ARC-I-008
	idem.New(h.Idem, func(*http.Request) string { return user }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.apply(w, r, p) }),
	).ServeHTTP(w, r)
}

// canOnSubject checks CapEdit on the deadline's subject (its matter) and writes the problem
// when it fails: a walled matter answers 404, a missing capability 403.
func (h *TriggerHTTP) canOnSubject(w http.ResponseWriter, r *http.Request, p registry.Principal, id deadline.DeadlineID) bool {
	d, err := h.Book.Get(r.Context(), id)
	switch {
	case errors.Is(err, deadline.ErrNotFound), errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such deadline")
		return false
	case err != nil:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return false
	}
	// rule: ARC-I-051
	// rule: ARC-I-042
	allowed, err := h.Access.Can(r.Context(), p, CapEdit, d.Proposed.Subject)
	switch {
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such deadline")
	case err != nil:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
	default:
		return true
	}
	return false
}

func (h *TriggerHTTP) apply(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	var b triggerBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	on, err := server.ParseBusinessDate(b.TriggerOn)
	if err != nil {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "trigger_on is not a date")
		return
	}
	id := deadline.DeadlineID(chi.URLParam(r, "deadline_id"))
	if !h.canOnSubject(w, r, p, id) {
		return
	}
	err = h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		return h.Book.ChangeTrigger(ctx, tx, id, on)
	})
	switch {
	case errors.Is(err, deadline.ErrNotFound), errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such deadline")
		return
	case err != nil:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(triggerView{DeadlineID: string(id), TriggerOn: on.String()})
}
