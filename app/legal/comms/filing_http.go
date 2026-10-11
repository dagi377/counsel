package comms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapFile is the capability to record court filings and file emails to a matter.
const CapFile = "documents.edit"

// Problem codes of the filing routes.
const (
	CodeUnavailable       = "unavailable"
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
)

// FilingWrites are the court filing write points the routes call; *FilingStore implements it.
type FilingWrites interface {
	IssueClaim(ctx context.Context, id, courtFileNo string, issuedOn time.Time) (Filing, error)
	RecordAffidavitOfService(ctx context.Context, f Filing) (Filing, error)
	// MatterOfFiling returns the matter a filing belongs to; ErrNotFound when there is none.
	MatterOfFiling(ctx context.Context, id string) (string, error)
}

// EmailFiler files an email to a matter; *EmailFilingStore implements it.
type EmailFiler interface {
	FileEmail(ctx context.Context, commItemID, matterID, filedBy string) (bool, error)
}

// Access is identity.Access as these routes use it.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

var (
	_ FilingWrites = (*FilingStore)(nil)
	_ EmailFiler   = (*EmailFilingStore)(nil)
)

// FilingHTTP serves the filing write routes (Q-EVENTS-WIRE-2):
//
//	POST /filings/{filing_id}/issue          claim.issued
//	POST /filings/affidavits-of-service      service.affidavit_received
//	POST /emails/{comm_item_id}/filings      email.filed
//
// Every POST needs an Idempotency-Key. A route answers 503 and calls nothing while a
// dependency is missing.
type FilingHTTP struct {
	Filings  FilingWrites
	Emails   EmailFiler
	Access   Access
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

func init() { add(registerFilingRoutes) }

// registerFilingRoutes mounts the filing routes; boot wiring supplies the dependencies and
// until it does they answer 503.
func registerFilingRoutes(r registry.Registry) error {
	r.Routes((&FilingHTTP{}).Routes)
	return nil
}

// Routes mounts the filing routes with their write declarations.
func (h *FilingHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// Recording court evidence and filing an email are not gate-owned.
	r.Method(http.MethodPost, "/filings/{filing_id}/issue", server.Declare(h.issue, EventClaimIssued))
	r.Method(http.MethodPost, "/filings/affidavits-of-service", server.Declare(h.affidavit, EventAffidavitOfServiceReceived))
	r.Method(http.MethodPost, "/emails/{comm_item_id}/filings", server.Declare(h.fileEmail, EventEmailFiled))
}

func (h *FilingHTTP) ready() bool {
	return h.Filings != nil && h.Emails != nil && h.Access != nil && h.Idem != nil &&
		h.Problems != nil && h.Principal != nil
}

func (h *FilingHTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	pw := h.Problems
	if pw == nil {
		pw = &server.Problems{}
	}
	pw.WriteProblem(w, r, server.NewProblem(status, code, title))
}

// writeErr maps a store error to a problem. Error text never reaches the body.
func (h *FilingHTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
	case errors.Is(err, ErrInvalid):
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Request is not valid")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

// guard answers 503, 401, 403 or 404 itself, then requires the Idempotency-Key and runs fn
// once per key. matterID, when set, is checked so a walled matter answers 404.
func (h *FilingHTTP) guard(w http.ResponseWriter, r *http.Request, fn func(w http.ResponseWriter, r *http.Request, p registry.Principal)) {
	if !h.ready() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return
	}
	p, ok := h.Principal(r)
	if !ok {
		h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return
	}
	if !h.can(w, r, p, registry.Subject{}) {
		return
	}
	// rule: ARC-I-007
	// The key is required before any store call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return
	}
	// rule: ARC-I-008
	idem.New(h.Idem, func(*http.Request) string { return userOf(p) }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fn(w, r, p) }),
	).ServeHTTP(w, r)
}

// can checks CapFile on s and writes the problem when it fails.
func (h *FilingHTTP) can(w http.ResponseWriter, r *http.Request, p registry.Principal, s registry.Subject) bool {
	// rule: ARC-I-051
	// Fail closed: an error or a missing capability stops the request.
	allowed, err := h.Access.Can(r.Context(), p, CapFile, s)
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
	case err != nil:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
	default:
		return true
	}
	return false
}

// userOf is the user a principal acts as: an agent run acts for its user.
func userOf(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}

type filingView struct {
	ID         string `json:"id"`
	MatterID   string `json:"matter_id"`
	DocumentID string `json:"document_id,omitempty"`
	Court      string `json:"court"`
	Reference  string `json:"reference,omitempty"`
	Status     string `json:"status"`
	FiledOn    string `json:"filed_on,omitempty"`
}

func filingViewOf(f Filing, loc *time.Location) filingView {
	v := filingView{ID: f.ID, MatterID: f.MatterID, DocumentID: f.DocumentID, Court: f.Court,
		Reference: f.Reference, Status: string(f.Status)}
	if !f.FiledOn.IsZero() {
		v.FiledOn = server.BusinessDateOf(f.FiledOn, loc).String()
	}
	return v
}

// dateIn parses a business date and returns its start in the request clock's zone.
func dateIn(ctx context.Context, s string) (time.Time, *time.Location, error) {
	d, err := server.ParseBusinessDate(s)
	if err != nil {
		return time.Time{}, nil, err
	}
	c, err := clock.From(ctx)
	if err != nil {
		return time.Time{}, nil, err
	}
	return d.Start(c.Location()), c.Location(), nil
}

type issueBody struct {
	CourtFileNo string `json:"court_file_no"`
	IssuedOn    string `json:"issued_on"`
}

// issue serves POST /filings/{filing_id}/issue: the court issued the filed claim.
func (h *FilingHTTP) issue(w http.ResponseWriter, r *http.Request) {
	h.guard(w, r, func(w http.ResponseWriter, r *http.Request, p registry.Principal) {
		var b issueBody
		if err := decodeStrict(r, &b); err != nil || strings.TrimSpace(b.CourtFileNo) == "" {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
			return
		}
		on, loc, err := dateIn(r.Context(), b.IssuedOn)
		if err != nil {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "issued_on is not a date")
			return
		}
		id := chi.URLParam(r, "filing_id")
		matterID, err := h.Filings.MatterOfFiling(r.Context(), id)
		if err != nil {
			h.writeErr(w, r, err)
			return
		}
		// rule: ARC-I-042
		// The filing's matter is checked: a walled matter answers 404, a missing capability 403.
		if !h.can(w, r, p, registry.Subject{Kind: matterkind.Subject, ID: matterID}) {
			return
		}
		f, err := h.Filings.IssueClaim(r.Context(), id, b.CourtFileNo, on)
		if err != nil {
			h.writeErr(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, filingViewOf(f, loc))
	})
}

type affidavitBody struct {
	MatterID   string `json:"matter_id"`
	DocumentID string `json:"document_id"`
	Court      string `json:"court"`
	Reference  string `json:"reference,omitempty"`
	FiledOn    string `json:"filed_on"`
}

// affidavit serves POST /filings/affidavits-of-service: a filed affidavit of service.
func (h *FilingHTTP) affidavit(w http.ResponseWriter, r *http.Request) {
	h.guard(w, r, func(w http.ResponseWriter, r *http.Request, p registry.Principal) {
		var b affidavitBody
		if err := decodeStrict(r, &b); err != nil || b.MatterID == "" || b.DocumentID == "" || b.Court == "" {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
			return
		}
		on, loc, err := dateIn(r.Context(), b.FiledOn)
		if err != nil {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "filed_on is not a date")
			return
		}
		// rule: ARC-I-042
		// A matter hidden by a wall answers 404.
		if !h.can(w, r, p, registry.Subject{Kind: matterkind.Subject, ID: b.MatterID}) {
			return
		}
		f, err := h.Filings.RecordAffidavitOfService(r.Context(), Filing{MatterID: b.MatterID,
			DocumentID: b.DocumentID, Court: b.Court, Reference: b.Reference, Status: FilingFiled,
			FiledBy: userOf(p), FiledOn: on})
		if err != nil {
			h.writeErr(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, filingViewOf(f, loc))
	})
}

type fileEmailBody struct {
	MatterID string `json:"matter_id"`
}

type fileEmailView struct {
	CommItemID string `json:"comm_item_id"`
	MatterID   string `json:"matter_id"`
	Filed      bool   `json:"filed"`
}

// fileEmail serves POST /emails/{comm_item_id}/filings: 201 when this call filed the email,
// 200 when it was already filed to the matter.
func (h *FilingHTTP) fileEmail(w http.ResponseWriter, r *http.Request) {
	h.guard(w, r, func(w http.ResponseWriter, r *http.Request, p registry.Principal) {
		var b fileEmailBody
		if err := decodeStrict(r, &b); err != nil || b.MatterID == "" {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
			return
		}
		// rule: ARC-I-042
		if !h.can(w, r, p, registry.Subject{Kind: matterkind.Subject, ID: b.MatterID}) {
			return
		}
		id := chi.URLParam(r, "comm_item_id")
		filed, err := h.Emails.FileEmail(r.Context(), id, b.MatterID, userOf(p))
		if err != nil {
			h.writeErr(w, r, err)
			return
		}
		status := http.StatusOK
		if filed {
			status = http.StatusCreated
		}
		writeJSON(w, status, fileEmailView{CommItemID: id, MatterID: b.MatterID, Filed: filed})
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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
