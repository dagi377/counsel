package docs

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/eventroute"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapTemplates is the capability to maintain templates and court forms.
const CapTemplates = "documents.templates"

// RevisionWrites requests a revision; *Revisions implements it.
type RevisionWrites interface {
	MatterOf(ctx context.Context, documentID string) (string, error)
	Request(ctx context.Context, documentID, versionID, by string) error
}

// CourtFormWrites records a court form version; *CourtForms implements it.
type CourtFormWrites interface {
	Record(ctx context.Context, v CourtFormVersion) (bool, error)
}

var (
	_ RevisionWrites  = (*Revisions)(nil)
	_ CourtFormWrites = (*CourtForms)(nil)
)

// EventHTTP serves POST /documents/{document_id}/revision-requests
// (document.revision_requested) and POST /court-forms (court_form.changed), Q-EVENTS-WIRE-5.
// The revision route checks documents.edit on the document's matter: a walled matter answers
// 404, a missing capability 403.
type EventHTTP struct {
	eventroute.Route
	Revisions  RevisionWrites
	CourtForms CourtFormWrites
	// Now is the court form check time.
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
	// A revision request and a court form version are not gate-owned.
	r.Method(http.MethodPost, "/documents/{document_id}/revision-requests", server.Declare(h.revision, EventRevisionRequested))
	r.Method(http.MethodPost, "/court-forms", server.Declare(h.courtForm, EventCourtFormChanged))
}

type revisionBody struct {
	VersionID string `json:"version_id"`
}

type revisionView struct {
	DocumentID string `json:"document_id"`
	VersionID  string `json:"version_id"`
	Status     string `json:"status"`
}

func (h *EventHTTP) revision(w http.ResponseWriter, r *http.Request) {
	h.Serve(w, r, h.Revisions != nil, CapEdit, func(w http.ResponseWriter, r *http.Request, p registry.Principal) {
		var b revisionBody
		if err := eventroute.Decode(r, &b); err != nil || strings.TrimSpace(b.VersionID) == "" {
			h.Invalid(w, r, "Body is not valid")
			return
		}
		id := chi.URLParam(r, "document_id")
		matterID, err := h.Revisions.MatterOf(r.Context(), id)
		if err != nil {
			h.writeErr(w, r, err)
			return
		}
		if matterID != "" && !h.CanOnMatter(w, r, p, CapEdit, matterID) {
			return
		}
		if err := h.Revisions.Request(r.Context(), id, b.VersionID, eventroute.User(p)); err != nil {
			h.writeErr(w, r, err)
			return
		}
		eventroute.JSON(w, http.StatusOK, revisionView{DocumentID: id, VersionID: b.VersionID, Status: "draft"})
	})
}

type courtFormBody struct {
	Jurisdiction string `json:"jurisdiction,omitempty"`
	RuleSet      string `json:"rule_set"`
	FormNumber   string `json:"form_number"`
	Title        string `json:"title"`
	SourceURL    string `json:"source_url"`
	VersionDate  string `json:"version_date"`
	Checksum     string `json:"checksum"` // hex
}

type courtFormView struct {
	RuleSet     string `json:"rule_set"`
	FormNumber  string `json:"form_number"`
	VersionDate string `json:"version_date"`
	Added       bool   `json:"added"`
}

func (h *EventHTTP) courtForm(w http.ResponseWriter, r *http.Request) {
	h.Serve(w, r, h.CourtForms != nil && h.Now != nil, CapTemplates, func(w http.ResponseWriter, r *http.Request, _ registry.Principal) {
		var b courtFormBody
		if err := eventroute.Decode(r, &b); err != nil {
			h.Invalid(w, r, "Body is not valid")
			return
		}
		day, err := time.Parse(time.DateOnly, b.VersionDate)
		sum, herr := hex.DecodeString(b.Checksum)
		if err != nil || herr != nil || len(sum) == 0 {
			h.Invalid(w, r, "version_date or checksum is not valid")
			return
		}
		added, err := h.CourtForms.Record(r.Context(), CourtFormVersion{Jurisdiction: b.Jurisdiction, RuleSet: b.RuleSet,
			FormNumber: b.FormNumber, Title: b.Title, SourceURL: b.SourceURL, VersionDate: day, Checksum: sum, CheckedAt: h.Now()})
		if err != nil {
			h.writeErr(w, r, err)
			return
		}
		status := http.StatusOK
		if added {
			status = http.StatusCreated
		}
		eventroute.JSON(w, status, courtFormView{RuleSet: b.RuleSet, FormNumber: b.FormNumber, VersionDate: b.VersionDate, Added: added})
	})
}

// writeErr maps a write point error: a missing record 404, a bad field 400.
func (h *EventHTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNoRecord):
		h.Fail(w, r, http.StatusNotFound, eventroute.CodeNotFound, "Not found")
	case errors.Is(err, ErrInvalid):
		h.Invalid(w, r, "Body is not valid")
	default:
		h.Internal(w, r)
	}
}
