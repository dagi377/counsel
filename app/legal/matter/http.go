package matter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Capabilities the matter routes check (spec/legal/access.json).
const (
	// CapDirectory sees a matter's number and name only.
	CapDirectory = "matters.directory"
	// CapView sees a matter's full record.
	CapView = "matters.view"
	// CapEdit changes a matter's plain fields.
	CapEdit = "matters.edit"
	// CapOpen requests a matter opening.
	CapOpen = "matters.open"
)

// Problem codes the matter routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
)

// Access is the part of identity.Access the routes use.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// HTTP serves the matter routes. Every dependency is required; a route answers 503 and calls
// nothing while one is missing.
type HTTP struct {
	Query    Query
	Access   Access
	Cursors  api.CursorCodec
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// idPattern is the openapi Id format (uuid).
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Routes mounts the read routes. They write nothing, so they carry no write declaration.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/matters", h.list)
	r.Get("/matters/{matter_id}", h.get)
}

// view is a matter as the API returns it. A directory-only caller gets id, number and name;
// every content field is omitted.
type view struct {
	ID                  string `json:"id"`
	Number              string `json:"number"`
	ShortName           string `json:"short_name"`
	ClientID            string `json:"client_id,omitempty"`
	PracticeAreaID      string `json:"practice_area_id,omitempty"`
	ResponsibleLawyerID string `json:"responsible_lawyer_id,omitempty"`
	Status              string `json:"status,omitempty"`
	Visibility          string `json:"visibility,omitempty"`
	OpenDate            string `json:"open_date,omitempty"`
	RetentionClass      string `json:"retention_class,omitempty"`
	CreatedAt           string `json:"created_at,omitempty"`
	Version             int    `json:"version,omitempty"`
	CourtProvince       string `json:"court_province,omitempty"`
	PropertyProvince    string `json:"property_province,omitempty"`
}

func viewOf(m Matter, full bool) view {
	v := view{ID: string(m.ID), Number: m.Number, ShortName: m.ShortName}
	if !full {
		return v
	}
	v.ClientID = string(m.ClientID)
	v.PracticeAreaID = m.PracticeAreaID
	v.ResponsibleLawyerID = m.ResponsibleLawyerID
	v.Status = string(m.Status)
	v.Visibility = string(m.Visibility)
	if !m.OpenDate.IsZero() {
		v.OpenDate = m.OpenDate.Format(time.DateOnly)
	}
	v.RetentionClass = m.RetentionClass
	if !m.CreatedAt.IsZero() {
		v.CreatedAt = m.CreatedAt.UTC().Format(time.RFC3339)
	}
	v.Version = m.RowVersion
	v.CourtProvince = m.CourtProvince
	v.PropertyProvince = m.PropertyProvince
	return v
}

func (h *HTTP) ready() bool {
	return h.Query != nil && h.Access != nil && h.Cursors != nil && h.Problems != nil && h.Principal != nil
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
// may carry client data.
func (h *HTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, api.ErrBadCursor) {
		h.problems().WriteProblem(w, r, api.BadCursorProblem())
		return
	}
	h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
}

// principal answers 503 or 401 itself and returns false when the request cannot go on.
func (h *HTTP) principal(w http.ResponseWriter, r *http.Request) (registry.Principal, bool) {
	if !h.ready() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return registry.Principal{}, false
	}
	p, ok := h.Principal(r)
	if !ok {
		h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return registry.Principal{}, false
	}
	return p, true
}

// readLevel is how much p may read: full (matters.view), directory only, or nothing. A
// subject hidden by a wall returns identity.ErrNotVisible.
func (h *HTTP) readLevel(ctx context.Context, p registry.Principal, s registry.Subject) (full, any bool, err error) {
	ok, err := h.Access.Can(ctx, p, CapView, s)
	if err != nil {
		return false, false, err
	}
	if ok {
		return true, true, nil
	}
	ok, err = h.Access.Can(ctx, p, CapDirectory, s)
	if err != nil {
		return false, false, err
	}
	return false, ok, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// list serves GET /matters: the matters the caller can see, by number, one page at a time.
func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	// rule: ARC-I-051
	// Fail closed: no capability, no service call.
	full, allowed, err := h.readLevel(ctx, p, registry.Subject{})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if !allowed {
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
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
	all, err := h.Query.List(ctx, Filter{})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	// rule: ARC-I-042
	// Authorization runs once per request; walled and restricted matters never reach the
	// page because Query reads under the caller's RLS session.
	page := api.Page[view]{Items: []view{}}
	for _, m := range all {
		if cur.After != "" && m.Number <= cur.After {
			continue
		}
		if len(page.Items) == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: page.Items[len(page.Items)-1].Number, Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.Next = next
			break
		}
		page.Items = append(page.Items, viewOf(m, full))
	}
	writeJSON(w, http.StatusOK, page)
}

// get serves GET /matters/{matter_id}. A malformed id, an unknown matter and a matter hidden
// by a wall all answer 404, so a hidden matter's existence does not leak.
func (h *HTTP) get(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "matter_id")
	if !idPattern.MatchString(id) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such matter")
		return
	}
	// rule: ARC-I-051
	full, allowed, err := h.readLevel(ctx, p, registry.Subject{Kind: matterkind.Subject, ID: id})
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such matter")
		return
	case err != nil:
		h.writeErr(w, r, err)
		return
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return
	}
	m, err := h.Query.Get(ctx, ID(id))
	if errors.Is(err, ErrNotFound) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such matter")
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if full {
		server.SetETag(w, int64(m.RowVersion))
	}
	writeJSON(w, http.StatusOK, viewOf(m, full))
}
