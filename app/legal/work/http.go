// Package work serves the work-item routes of app/legal over the kernel work.Service.
package work

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
	kwork "github.com/dagi377/counsel/kernel/work"
)

// Capabilities the work-item routes check (spec/access.json).
const (
	// CapView reads work items.
	CapView = "work.view"
	// CapEdit creates and changes work items.
	CapEdit = "work.edit"
)

// Problem codes the work-item routes write.
const (
	CodeUnauthenticated      = "unauthenticated"
	CodeMissingCapability    = "missing_capability"
	CodeNotFound             = "not_found"
	CodeUnavailable          = "unavailable"
	CodeTransitionNotAllowed = "transition_not_allowed"
	CodeSelfApproval         = "self_approval"
)

// SubjectMatter is the work-item subject kind of a matter-scoped item (as the 6100 backfill
// and the agent work tools write it).
const SubjectMatter = "matter"

// itemTypes is the work.work_item.type CHECK set.
var itemTypes = map[string]bool{"task": true, "request": true, "review": true, "filing": true, "milestone": true, "bug": true}

// itemIDPattern bounds a path id before any service call; the store maps a non-uuid to 404.
var itemIDPattern = regexp.MustCompile(`^[0-9A-Za-z-]{1,64}$`)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Access is the part of identity.Access the routes use.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// TxRunner runs fn in one transaction; kernel/db's pool implements it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// HTTP serves GET /work-items, POST /work-items and PATCH /work-items/{work_item_id}. Every
// dependency is required; a route answers 503 and calls nothing while one is missing.
type HTTP struct {
	Items    kwork.Service
	DB       TxRunner
	Access   Access
	Cursors  api.CursorCodec
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// Routes mounts the work-item routes with their write declarations.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/work-items", h.list)
	// rule: ARC-I-012
	// Creating and moving work items is not gate-owned; an automation effect opens its own gate.
	r.Method(http.MethodPost, "/work-items", server.Declare(h.create, "work_item.created"))
	r.Method(http.MethodPatch, "/work-items/{work_item_id}", server.Declare(h.update,
		"work_item.status_id", "work_item.approver_id"))
}

type itemView struct {
	ID              string   `json:"id"`
	Key             string   `json:"key"`
	Type            string   `json:"type"`
	Title           string   `json:"title"`
	WorkflowID      string   `json:"workflow_id"`
	StatusID        string   `json:"status_id"`
	MatterID        string   `json:"matter_id,omitempty"`
	ParentID        string   `json:"parent_id,omitempty"`
	AssigneeUserID  string   `json:"assignee_user_id,omitempty"`
	AssigneeIsAgent bool     `json:"assignee_is_agent"`
	ApproverID      string   `json:"approver_id,omitempty"`
	ReporterID      string   `json:"reporter_id"`
	Priority        int      `json:"priority"`
	DueAt           string   `json:"due_at,omitempty"`
	Labels          []string `json:"labels"`
	EstimateMin     int      `json:"estimate_min,omitempty"`
	Version         int      `json:"version"`
}

func viewOf(it kwork.Item) itemView {
	v := itemView{ID: string(it.ID), Key: it.Key, Type: it.Type, Title: it.Title, WorkflowID: it.WorkflowID,
		StatusID: it.StatusID, ParentID: string(it.ParentID), AssigneeUserID: it.AssigneeUserID,
		AssigneeIsAgent: it.AssigneeIsAgent, ApproverID: it.ApproverID, ReporterID: it.ReporterID,
		Priority: it.Priority, Labels: it.Labels, EstimateMin: it.EstimateMin, Version: it.Version}
	if it.Subject.Kind == SubjectMatter {
		v.MatterID = it.Subject.ID
	}
	if it.DueAt != nil {
		v.DueAt = it.DueAt.UTC().Format(time.RFC3339)
	}
	if v.Labels == nil {
		v.Labels = []string{}
	}
	return v
}

func (h *HTTP) ready() bool {
	return h.Items != nil && h.DB != nil && h.Access != nil && h.Cursors != nil && h.Idem != nil &&
		h.Problems != nil && h.Principal != nil
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

// writeErr maps a service error to a problem. Error text never reaches the body.
func (h *HTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, api.ErrBadCursor):
		h.problems().WriteProblem(w, r, api.BadCursorProblem())
	case errors.Is(err, kwork.ErrNotFound), errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such work item")
	case errors.Is(err, kwork.ErrStale):
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
	case errors.Is(err, kwork.ErrTransitionNotAllowed):
		h.fail(w, r, http.StatusConflict, CodeTransitionNotAllowed, "The workflow has no such transition")
	case errors.Is(err, kwork.ErrSelfApproval):
		h.fail(w, r, http.StatusUnprocessableEntity, CodeSelfApproval, "The assignee or an agent cannot approve")
	case errors.Is(err, kwork.ErrInvalidItem):
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Work item is not valid")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

// authorize answers 503, 401, 403 or 404 itself; it runs before any service call.
func (h *HTTP) authorize(w http.ResponseWriter, r *http.Request, capability string) (registry.Principal, bool) {
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

// visible answers 404 (never 403) for an item on a matter hidden by a wall.
func (h *HTTP) visible(ctx context.Context, p registry.Principal, matterID string) error {
	if matterID == "" {
		return nil
	}
	// rule: ARC-I-042
	_, err := h.Access.Can(ctx, p, CapEdit, registry.Subject{Kind: matterkind.Subject, ID: matterID})
	return err
}

// actorOf is the user a request acts for: the user, or the user an agent acts for.
func actorOf(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}

// list pages work items. Walls and RLS limit the rows the service returns.
func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
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
	got, err := h.Items.List(r.Context(), kwork.Filter{After: kwork.ItemID(cur.After), Limit: cur.Limit + 1})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := api.Page[itemView]{Items: []itemView{}}
	for i, it := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: string(got[i-1].ID), Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.Next = next
			break
		}
		page.Items = append(page.Items, viewOf(it))
	}
	writeJSON(w, http.StatusOK, page)
}

type createBody struct {
	Type           string   `json:"type"`
	Title          string   `json:"title"`
	WorkflowID     string   `json:"workflow_id"`
	StatusID       string   `json:"status_id"`
	MatterID       string   `json:"matter_id,omitempty"`
	ParentID       string   `json:"parent_id,omitempty"`
	AssigneeUserID string   `json:"assignee_user_id,omitempty"`
	Priority       int      `json:"priority,omitempty"`
	DueAt          string   `json:"due_at,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	EstimateMin    int      `json:"estimate_min,omitempty"`
}

func optionalUUID(s string) bool { return s == "" || uuidPattern.MatchString(s) }

func (b createBody) valid() bool {
	return itemTypes[b.Type] && strings.TrimSpace(b.Title) != "" && uuidPattern.MatchString(b.WorkflowID) &&
		uuidPattern.MatchString(b.StatusID) && optionalUUID(b.MatterID) && optionalUUID(b.ParentID) &&
		optionalUUID(b.AssigneeUserID) && b.Priority >= 0 && b.Priority <= 5 && b.EstimateMin >= 0
}

// create serves POST /work-items: the caller reports a new work item.
func (h *HTTP) create(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r, CapEdit)
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
	idem.New(h.Idem, func(*http.Request) string { return actorOf(p) }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.record(w, r, p) }),
	).ServeHTTP(w, r)
}

func (h *HTTP) record(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	var b createBody
	if err := decodeStrict(r, &b); err != nil || !b.valid() {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	var due *time.Time
	if b.DueAt != "" {
		t, err := time.Parse(time.RFC3339, b.DueAt)
		if err != nil {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "due_at is not an RFC 3339 time")
			return
		}
		due = &t
	}
	if err := h.visible(r.Context(), p, b.MatterID); err != nil {
		h.writeErr(w, r, err)
		return
	}
	it := kwork.Item{Type: b.Type, Title: strings.TrimSpace(b.Title), WorkflowID: b.WorkflowID, StatusID: b.StatusID,
		ParentID: kwork.ItemID(b.ParentID), AssigneeUserID: b.AssigneeUserID, ReporterID: actorOf(p),
		Priority: b.Priority, DueAt: due, Labels: b.Labels, EstimateMin: b.EstimateMin}
	if b.MatterID != "" {
		it.Subject = kwork.Subject{Kind: SubjectMatter, ID: strings.ToLower(b.MatterID)}
	}
	var id kwork.ItemID
	err := h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = h.Items.Create(ctx, tx, it)
		return err
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	out, err := h.Items.Get(r.Context(), id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(out.Version))
	writeJSON(w, http.StatusCreated, viewOf(out))
}

type patchBody struct {
	StatusID   *string `json:"status_id,omitempty"`
	ApproverID *string `json:"approver_id,omitempty"`
}

// update serves PATCH /work-items/{work_item_id}: a status move along the workflow and/or a
// new approver. The approver is never the assignee or an agent (the service refuses both).
func (h *HTTP) update(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r, CapEdit)
	if !ok {
		return
	}
	id := chi.URLParam(r, "work_item_id")
	var b patchBody
	if !itemIDPattern.MatchString(id) || decodeStrict(r, &b) != nil || (b.StatusID == nil && b.ApproverID == nil) ||
		(b.StatusID != nil && !uuidPattern.MatchString(*b.StatusID)) ||
		(b.ApproverID != nil && !uuidPattern.MatchString(*b.ApproverID)) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	// rule: ARC-I-006
	// A missing If-Match is refused before any service call.
	if strings.TrimSpace(r.Header.Get("If-Match")) == "" {
		h.fail(w, r, http.StatusPreconditionRequired, server.CodePreconditionRequired, "If-Match is required")
		return
	}
	ctx := r.Context()
	cur, err := h.Items.Get(ctx, kwork.ItemID(id))
	if err == nil && cur.Subject.Kind == SubjectMatter {
		err = h.visible(ctx, p, cur.Subject.ID)
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if err := server.CheckIfMatch(r, int64(cur.Version)); err != nil {
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	}
	err = h.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// Move checks the caller's version, so it runs before SetApprover bumps it.
		if b.StatusID != nil && *b.StatusID != cur.StatusID {
			if err := h.Items.Move(ctx, tx, cur.ID, cur.Version, *b.StatusID, actorOf(p)); err != nil {
				return err
			}
		}
		if b.ApproverID != nil {
			// rule: ARC-I-051
			// An approver is always a person; the service refuses the assignee.
			return h.Items.SetApprover(ctx, tx, cur.ID, *b.ApproverID, false)
		}
		return nil
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	out, err := h.Items.Get(ctx, cur.ID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(out.Version))
	writeJSON(w, http.StatusOK, viewOf(out))
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
