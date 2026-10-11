package trust

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapRequest prepares trust requests (spec/access.json). Listing them needs CapView; the
// problem codes are the read routes' (http.go).
const CapRequest = "trust.request"

// RequestAccess is the part of identity.Access the trust request routes use.
type RequestAccess interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// RequestTx runs fn in one transaction; kernel/db's pool implements it.
type RequestTx interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// RequestHTTP serves GET /trust/requests and POST /trust/requests. Every dependency is
// required; a route answers 503 and calls nothing while one is missing.
type RequestHTTP struct {
	Requests Requests
	DB       RequestTx
	Access   RequestAccess
	Cursors  api.CursorCodec
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

var requestUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Routes mounts the trust request routes with their write declarations.
func (h *RequestHTTP) Routes(r chi.Router) {
	r.Get("/trust/requests", h.list)
	// rule: ARC-I-012
	// Preparing a draft request is not gate-owned; approval is a separate step.
	r.Method(http.MethodPost, "/trust/requests", server.Declare(h.create, "trust_request.prepared"))
}

type requestView struct {
	ID              string `json:"id"`
	MatterID        string `json:"matter_id"`
	AmountCents     int64  `json:"amount_cents"`
	Reason          string `json:"reason,omitempty"`
	DueOn           string `json:"due_on,omitempty"`
	Status          string `json:"status"`
	PreparedBy      string `json:"prepared_by"`
	PreparedByAgent bool   `json:"prepared_by_agent"`
	ApprovedBy      string `json:"approved_by,omitempty"`
	Version         int    `json:"version"`
}

func requestViewOf(r Request) requestView {
	v := requestView{ID: string(r.ID), MatterID: r.MatterID, AmountCents: r.AmountCents, Reason: r.Reason,
		Status: string(r.Status), PreparedBy: r.PreparedBy, PreparedByAgent: r.PreparedByAgent,
		ApprovedBy: r.ApprovedBy, Version: r.RowVersion}
	if !r.DueOn.IsZero() {
		v.DueOn = r.DueOn.Format(time.DateOnly)
	}
	return v
}

func (h *RequestHTTP) ready() bool {
	return h.Requests != nil && h.DB != nil && h.Access != nil && h.Cursors != nil && h.Idem != nil &&
		h.Problems != nil && h.Principal != nil
}

func (h *RequestHTTP) problems() server.ProblemWriter {
	if h.Problems == nil {
		return &server.Problems{}
	}
	return h.Problems
}

func (h *RequestHTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	h.problems().WriteProblem(w, r, server.NewProblem(status, code, title))
}

// writeErr maps a service error to a problem. Error text never reaches the body.
func (h *RequestHTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, api.ErrBadCursor):
		h.problems().WriteProblem(w, r, api.BadCursorProblem())
	case errors.Is(err, ErrNotFound), errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such trust request")
	case errors.Is(err, ErrInvalid):
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Trust request is not valid")
	case errors.Is(err, ErrAgentApproval), errors.Is(err, ErrSelfApproval):
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Not allowed for this trust request")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

// authorize answers 503, 401, 403 or 404 itself; it runs before any service call.
func (h *RequestHTTP) authorize(w http.ResponseWriter, r *http.Request, capability string) (registry.Principal, bool) {
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
	allowed, err := h.Access.Can(r.Context(), p, capability, registry.Subject{})
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
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

func requestActorOf(p registry.Principal) RequestActor {
	if p.Kind == registry.PrincipalAgent {
		return RequestActor{UserID: p.ActingFor, Agent: true}
	}
	return RequestActor{UserID: p.UserID}
}

// list pages trust requests. Walls and RLS limit the rows the store returns.
func (h *RequestHTTP) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r, CapView); !ok {
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
	got, err := h.Requests.List(r.Context(), RequestFilter{After: RequestID(cur.After), Limit: cur.Limit + 1})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := api.Page[requestView]{Items: []requestView{}}
	for i, q := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: string(got[i-1].ID), Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.Next = next
			break
		}
		page.Items = append(page.Items, requestViewOf(q))
	}
	writeJSON(w, http.StatusOK, page)
}

type requestBody struct {
	MatterID    string `json:"matter_id"`
	AmountCents int64  `json:"amount_cents"`
	Reason      string `json:"reason,omitempty"`
	DueOn       string `json:"due_on,omitempty"`
}

// create serves POST /trust/requests: the caller prepares a draft request.
func (h *RequestHTTP) create(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r, CapRequest)
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
	idem.New(h.Idem, func(*http.Request) string { return requestActorOf(p).UserID }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.prepare(w, r, p) }),
	).ServeHTTP(w, r)
}

func (h *RequestHTTP) prepare(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	var b requestBody
	if err := decodeRequestBody(r, &b); err != nil || !requestUUID.MatchString(b.MatterID) || b.AmountCents <= 0 {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	var due time.Time
	if b.DueOn != "" {
		d, err := time.Parse(time.DateOnly, b.DueOn)
		if err != nil {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "due_on is not a date")
			return
		}
		due = d
	}
	// rule: ARC-I-042
	// A matter hidden by a wall answers 404.
	if _, err := h.Access.Can(r.Context(), p, CapRequest, registry.Subject{Kind: matterkind.Subject, ID: b.MatterID}); err != nil {
		h.writeErr(w, r, err)
		return
	}
	q := Request{MatterID: b.MatterID, AmountCents: b.AmountCents, Reason: b.Reason, DueOn: due}
	var id RequestID
	err := h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = h.Requests.Create(ctx, tx, q, requestActorOf(p))
		return err
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	out, err := h.Requests.Get(r.Context(), id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(out.RowVersion))
	writeJSON(w, http.StatusCreated, requestViewOf(out))
}

func decodeRequestBody(r *http.Request, v any) error {
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
