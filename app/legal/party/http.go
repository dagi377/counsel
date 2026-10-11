package party

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Capabilities the contact routes check (spec/legal/access.json).
const (
	// CapView reads contacts.
	CapView = "contacts.view"
	// CapEdit creates and changes contacts.
	CapEdit = "contacts.edit"
)

// SubjectKind is the access subject kind of a contact.
const SubjectKind = "party.contact"

// Problem codes the contact routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
	CodeDuplicateContact  = "duplicate_contact"
)

// Access is the part of identity.Access the routes use.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// HTTP serves GET /contacts, POST /contacts and GET /contacts/{contact_id}. Every dependency
// is required; a route answers 503 and calls nothing while one is missing.
type HTTP struct {
	Contacts Contacts
	Access   Access
	Cursors  api.CursorCodec
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// idPattern is the openapi Id format (uuid).
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Routes mounts the contact routes. POST /contacts declares the one state key it writes.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/contacts", h.list)
	// rule: ARC-I-012
	// A new contact is proposed; no gate owns contact creation.
	r.Method(http.MethodPost, "/contacts", server.Declare(h.create, "contact.created"))
	r.Get("/contacts/{contact_id}", h.get)
}

// view is a contact as the API returns it.
type view struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	LegalName         string `json:"legal_name"`
	DisplayName       string `json:"display_name"`
	PreferredLanguage string `json:"preferred_language"`
	IsClient          bool   `json:"is_client"`
	State             string `json:"state"`
	CreatedAt         string `json:"created_at,omitempty"`
	Version           int    `json:"version"`
}

func viewOf(c Contact) view {
	v := view{ID: string(c.ID), Kind: string(c.Kind), LegalName: c.LegalName, DisplayName: c.DisplayName,
		PreferredLanguage: c.PreferredLanguage, IsClient: c.IsClient, State: string(c.State), Version: c.RowVersion}
	if !c.CreatedAt.IsZero() {
		v.CreatedAt = c.CreatedAt.UTC().Format(time.RFC3339)
	}
	return v
}

func (h *HTTP) ready() bool {
	return h.Contacts != nil && h.Access != nil && h.Cursors != nil && h.Problems != nil && h.Principal != nil
}

func (h *HTTP) problems() server.ProblemWriter {
	if h.Problems == nil {
		return &server.Problems{}
	}
	return h.Problems
}

func (h *HTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	h.problems().WriteProblem(w, r, server.NewProblem(status, code, title))
}

// writeErr maps an unexpected error to a problem. The error text never reaches the body: it
// may carry client names.
func (h *HTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, api.ErrBadCursor) {
		h.problems().WriteProblem(w, r, api.BadCursorProblem())
		return
	}
	h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
}

// authorize answers 503, 401, 403 or 404 itself and returns false when the request cannot go
// on. It runs before any service call.
func (h *HTTP) authorize(w http.ResponseWriter, r *http.Request, capability string, s registry.Subject) (registry.Principal, bool) {
	if !h.ready() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return registry.Principal{}, false
	}
	p, ok := h.Principal(r)
	if !ok {
		h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return registry.Principal{}, false
	}
	// rule: ARC-I-051
	// Fail closed: an error or a missing capability stops the request.
	allowed, err := h.Access.Can(r.Context(), p, capability, s)
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such contact")
		return registry.Principal{}, false
	case err != nil:
		h.writeErr(w, r, err)
		return registry.Principal{}, false
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return registry.Principal{}, false
	}
	return p, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// list serves GET /contacts: contacts by id, one page at a time.
func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r, CapView, registry.Subject{}); !ok {
		return
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
	got, err := h.Contacts.List(r.Context(), Filter{After: ContactID(cur.After), Limit: cur.Limit + 1})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := api.Page[view]{Items: []view{}}
	for i, c := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: string(got[i-1].ID), Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.Next = next
			break
		}
		page.Items = append(page.Items, viewOf(c))
	}
	writeJSON(w, http.StatusOK, page)
}

type createBody struct {
	Kind              string `json:"kind"`
	LegalName         string `json:"legal_name"`
	DisplayName       string `json:"display_name"`
	PreferredLanguage string `json:"preferred_language,omitempty"`
	IsClient          bool   `json:"is_client,omitempty"`
}

func decodeStrict(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

// create serves POST /contacts. The new contact is proposed.
func (h *HTTP) create(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r, CapEdit, registry.Subject{})
	if !ok {
		return
	}
	var b createBody
	if err := decodeStrict(r, &b); err != nil || b.Kind == "" || b.LegalName == "" || b.DisplayName == "" {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	by := p.UserID
	if p.Kind == registry.PrincipalAgent {
		by = p.ActingFor
	}
	c, err := h.Contacts.Create(r.Context(), NewContact{Kind: Kind(b.Kind), LegalName: b.LegalName,
		DisplayName: b.DisplayName, PreferredLanguage: b.PreferredLanguage, IsClient: b.IsClient, CreatedBy: by})
	switch {
	case errors.Is(err, ErrDuplicate):
		h.fail(w, r, http.StatusConflict, CodeDuplicateContact, "Contact already exists")
		return
	case errors.Is(err, ErrInvalid):
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Contact is not valid")
		return
	case err != nil:
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(c.RowVersion))
	writeJSON(w, http.StatusCreated, viewOf(c))
}

// get serves GET /contacts/{contact_id}. A contact hidden by a wall or RLS answers 404.
func (h *HTTP) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "contact_id")
	if !idPattern.MatchString(id) {
		if _, ok := h.authorize(w, r, CapView, registry.Subject{}); !ok {
			return
		}
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "contact_id is not valid")
		return
	}
	if _, ok := h.authorize(w, r, CapView, registry.Subject{Kind: SubjectKind, ID: id}); !ok {
		return
	}
	c, err := h.Contacts.Get(r.Context(), ContactID(id))
	if errors.Is(err, ErrNotFound) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such contact")
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(c.RowVersion))
	writeJSON(w, http.StatusOK, viewOf(c))
}
