package docs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/api"
	kdocs "github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Capabilities the document routes check (spec/legal/access.json).
const (
	// CapView reads documents and their page text.
	CapView = "documents.view"
	// CapEdit uploads documents.
	CapEdit = "documents.edit"
)

// SubjectKind is the access subject kind of a document.
const SubjectKind = "docs.document"

// Problem codes the document routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
	// CodeUnknownKind is the kernel/docs code for an unregistered document kind.
	CodeUnknownKind = "docs.unknown_kind"
)

// maxUploadBytes caps one upload body (the base64 content plus its envelope).
const maxUploadBytes = 32 << 20

// ListFilter selects one page of documents, ordered by id. Zero fields do not filter.
type ListFilter struct {
	After    string // return documents with id greater than this
	Limit    int
	MatterID string
	Kind     string
}

// Upload is one new document: its first version's bytes, filed in a matter folder.
type Upload struct {
	MatterID string
	Kind     string
	FolderID FolderID
	Filename string
	Content  []byte
	// By is the user the upload is recorded for (the acting user for an agent).
	By string
	// IdempotencyKey is the request's Idempotency-Key.
	IdempotencyKey string
}

// DocumentStore is the document port the routes read and write through. Boot implements it
// over kernel/docs Store.Put and Folders.File in one transaction; tests use a fake.
type DocumentStore interface {
	// List returns up to f.Limit documents matching f, ordered by id.
	List(ctx context.Context, f ListFilter) ([]Document, error)
	// Document returns one filed document; ErrNotFound when there is none.
	Document(ctx context.Context, id string) (Document, error)
	// Upload stores the first version of a new document and files it. An unregistered kind
	// returns ErrUnknownKind (or kernel/docs ErrUnknownKind).
	Upload(ctx context.Context, u Upload) (Document, kdocs.VersionID, error)
	// HasVersion reports whether v is a version of document id.
	HasVersion(ctx context.Context, id string, v kdocs.VersionID) (bool, error)
}

// Pages is the part of kernel/docs Store the page route reads.
type Pages interface {
	Lines(ctx context.Context, v kdocs.VersionID, page int) ([]kdocs.Line, error)
}

// Access is the part of identity.Access the routes use.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// HTTP serves GET /documents, POST /documents and
// GET /documents/{document_id}/versions/{version_id}/pages/{page}. Every dependency is
// required; a route answers 503 and calls nothing while one is missing.
type HTTP struct {
	Documents DocumentStore
	Pages     Pages
	Access    Access
	Cursors   api.CursorCodec
	Idem      idem.Store
	Problems  server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

func init() { add(registerRoutes) }

// registerRoutes mounts the document routes through Registry.Routes. Boot wiring supplies
// the dependencies; until it does, the routes answer 503.
func registerRoutes(r registry.Registry) error {
	r.Routes((&HTTP{}).Routes)
	return nil
}

// idPattern is the openapi Id format (uuid).
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Routes mounts the document routes. POST /documents declares the one state key it writes.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/documents", h.list)
	// rule: ARC-I-012
	// An upload stores a draft version; no gate owns it.
	r.Method(http.MethodPost, "/documents", server.Declare(h.create, "document.uploaded"))
	r.Get("/documents/{document_id}/versions/{version_id}/pages/{page}", h.page)
}

func (h *HTTP) ready() bool {
	return h.Documents != nil && h.Pages != nil && h.Access != nil && h.Cursors != nil && h.Idem != nil &&
		h.Problems != nil && h.Principal != nil
}

func (h *HTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	pw := h.Problems
	if pw == nil {
		pw = &server.Problems{}
	}
	pw.WriteProblem(w, r, server.NewProblem(status, code, title))
}

// writeErr maps an error to a problem; the error text never reaches the body.
func (h *HTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, api.ErrBadCursor):
		h.Problems.WriteProblem(w, r, api.BadCursorProblem())
	case errors.Is(err, ErrUnknownKind), errors.Is(err, kdocs.ErrUnknownKind):
		h.fail(w, r, http.StatusUnprocessableEntity, CodeUnknownKind, "Unregistered document kind")
	case errors.Is(err, ErrNotFound), errors.Is(err, kdocs.ErrNotFound), errors.Is(err, kdocs.ErrBadRange):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such document page")
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrWrongMatter):
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Document is not valid")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
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
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such document")
		return registry.Principal{}, false
	case err != nil:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
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

// documentView is a document as the API returns it.
type documentView struct {
	ID        string `json:"id"`
	MatterID  string `json:"matter_id,omitempty"`
	Kind      string `json:"kind"`
	FolderID  string `json:"folder_id,omitempty"`
	VersionID string `json:"version_id,omitempty"`
}

func viewOf(d Document) documentView {
	return documentView{ID: d.ID, MatterID: d.MatterID, Kind: d.Kind, FolderID: string(d.FolderID)}
}

// documentPage is the listDocuments body.
type documentPage struct {
	Data       []documentView `json:"data"`
	NextCursor *string        `json:"next_cursor"`
}

// list serves GET /documents: documents by id, one page at a time, optionally of one matter.
func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	matter := q.Get("matter_id")
	subject := registry.Subject{}
	if idPattern.MatchString(matter) {
		subject = registry.Subject{Kind: matterkind.Subject, ID: matter}
	}
	if _, ok := h.authorize(w, r, CapView, subject); !ok {
		return
	}
	if matter != "" && subject.ID == "" {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "matter_id is not valid")
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
	got, err := h.Documents.List(r.Context(), ListFilter{After: cur.After, Limit: cur.Limit + 1, MatterID: matter,
		Kind: q.Get("kind")})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := documentPage{Data: []documentView{}}
	for i, d := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: got[i-1].ID, Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.NextCursor = &next
			break
		}
		page.Data = append(page.Data, viewOf(d))
	}
	writeJSON(w, http.StatusOK, page)
}

// uploadBody is the POST /documents body. Content is the file's bytes, base64 encoded.
type uploadBody struct {
	MatterID string `json:"matter_id"`
	Kind     string `json:"kind"`
	FolderID string `json:"folder_id"`
	Filename string `json:"filename,omitempty"`
	Content  []byte `json:"content"`
}

func (b uploadBody) valid() bool {
	return idPattern.MatchString(b.MatterID) && idPattern.MatchString(b.FolderID) && b.Kind != "" &&
		len(b.Kind) <= 64 && len(b.Filename) <= 255 && len(b.Content) > 0
}

func decodeStrict(body []byte, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

// create serves POST /documents: a new document's first version, filed in a matter folder.
func (h *HTTP) create(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r, CapEdit, registry.Subject{})
	if !ok {
		return
	}
	// rule: ARC-I-007
	// The key is required before any service call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return
	}
	// rule: ARC-I-008
	idem.New(h.Idem, func(*http.Request) string { return p.UserID + p.RunID }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.upload(w, r, p) }),
	).ServeHTTP(w, r)
}

func (h *HTTP) upload(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxUploadBytes+1))
	var b uploadBody
	if err != nil || len(raw) > maxUploadBytes || decodeStrict(raw, &b) != nil || !b.valid() {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	// The matter decides visibility: a walled matter answers 404, never 403.
	if _, ok := h.authorize(w, r, CapEdit, registry.Subject{Kind: matterkind.Subject, ID: b.MatterID}); !ok {
		return
	}
	// rule: ARC-I-051
	// An unregistered kind is refused before any bytes are stored.
	if !knownKind(b.Kind) {
		h.fail(w, r, http.StatusUnprocessableEntity, CodeUnknownKind, "Unregistered document kind")
		return
	}
	by := p.UserID
	if p.Kind == registry.PrincipalAgent {
		by = p.ActingFor
	}
	d, v, err := h.Documents.Upload(r.Context(), Upload{MatterID: b.MatterID, Kind: b.Kind, FolderID: FolderID(b.FolderID),
		Filename: b.Filename, Content: b.Content, By: by, IdempotencyKey: r.Header.Get(idem.HeaderName)})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	out := viewOf(d)
	out.VersionID = string(v)
	writeJSON(w, http.StatusCreated, out)
}

// lineView is one numbered line of page text.
type lineView struct {
	No   int    `json:"no"`
	Text string `json:"text"`
}

// pageView is the getDocumentPage body: line-numbered text, the citation source.
type pageView struct {
	DocumentID string     `json:"document_id"`
	VersionID  string     `json:"version_id"`
	Page       int        `json:"page"`
	Lines      []lineView `json:"lines"`
}

// page serves GET /documents/{document_id}/versions/{version_id}/pages/{page}. A page out of
// range, a version of another document and a walled document all answer 404.
func (h *HTTP) page(w http.ResponseWriter, r *http.Request) {
	id, vid := chi.URLParam(r, "document_id"), chi.URLParam(r, "version_id")
	n, err := strconv.Atoi(chi.URLParam(r, "page"))
	if !idPattern.MatchString(id) || !idPattern.MatchString(vid) || err != nil || n < 1 {
		if _, ok := h.authorize(w, r, CapView, registry.Subject{}); !ok {
			return
		}
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Path is not valid")
		return
	}
	if _, ok := h.authorize(w, r, CapView, registry.Subject{Kind: SubjectKind, ID: id}); !ok {
		return
	}
	v := kdocs.VersionID(vid)
	owned, err := h.Documents.HasVersion(r.Context(), id, v)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if !owned {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such document page")
		return
	}
	lines, err := h.Pages.Lines(r.Context(), v, n)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	out := pageView{DocumentID: id, VersionID: vid, Page: n, Lines: []lineView{}}
	for _, l := range lines {
		out.Lines = append(out.Lines, lineView{No: l.No, Text: l.Text})
	}
	writeJSON(w, http.StatusOK, out)
}
