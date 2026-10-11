package timekeeping

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

// Capabilities the time routes check (spec/legal/access.json).
const (
	// CapRecord records and edits one's own time and expenses.
	CapRecord = "time.record"
	// CapViewAll sees and edits every timekeeper's entries.
	CapViewAll = "time.view_all"
)

// Problem codes the time routes write.
const (
	CodeUnauthenticated    = "unauthenticated"
	CodeMissingCapability  = "missing_capability"
	CodeNotFound           = "not_found"
	CodeUnavailable        = "unavailable"
	CodeLockedToBill       = "locked_to_bill"
	CodeActivityOutOfScope = "activity_out_of_scope"
)

const defaultProblemTitleBody = "Body is not valid"

// Access is the part of identity.Access the routes use.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// TxRunner runs fn in one transaction; kernel/db's pool implements it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// HTTP serves GET /time-entries, POST /time-entries and PATCH /time-entries/{time_entry_id}.
// Every dependency is required; a route answers 503 and calls nothing while one is missing.
type HTTP struct {
	Entries  Entries
	DB       TxRunner
	Access   Access
	Cursors  api.CursorCodec
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

var (
	entryIDPattern = regexp.MustCompile(`^[0-9A-Za-z-]{1,64}$`)
	uuidPattern    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// Routes mounts the time-entry routes with their write declarations.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/time-entries", h.list)
	// rule: ARC-I-012
	// Recording and editing time are not gate-owned; approval and billing are.
	r.Method(http.MethodPost, "/time-entries", server.Declare(h.create, "time_entry.recorded"))
	r.Method(http.MethodPatch, "/time-entries/{time_entry_id}", server.Declare(h.update,
		"time_entry.minutes", "time_entry.narrative", "time_entry.activity_code"))
}

type entryView struct {
	ID            string   `json:"id"`
	MatterID      string   `json:"matter_id"`
	UserID        string   `json:"user_id"`
	WorkDate      string   `json:"work_date"`
	Minutes       int      `json:"minutes"`
	BilledMinutes int      `json:"billed_minutes"`
	ActivityCode  string   `json:"activity_code,omitempty"`
	Narrative     string   `json:"narrative"`
	Billable      bool     `json:"billable"`
	Origin        string   `json:"origin"`
	State         string   `json:"state"`
	BillID        string   `json:"bill_id,omitempty"`
	Flags         []string `json:"flags,omitempty"`
	Version       int      `json:"version"`
}

func entryViewOf(e TimeEntry) entryView {
	return entryView{ID: string(e.ID), MatterID: e.MatterID, UserID: e.UserID, WorkDate: e.WorkDate.Format(time.DateOnly),
		Minutes: e.Minutes, BilledMinutes: e.BilledMinutes, ActivityCode: e.ActivityCode, Narrative: e.Narrative,
		Billable: e.Billable, Origin: string(e.Origin), State: string(e.State), BillID: e.BillID, Flags: e.Flags,
		Version: e.RowVersion}
}

func (h *HTTP) ready() bool {
	return h.Entries != nil && h.DB != nil && h.Access != nil && h.Cursors != nil && h.Idem != nil &&
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
	case errors.Is(err, ErrNotFound), errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such time entry")
	case errors.Is(err, ErrBilledLocked):
		h.fail(w, r, http.StatusConflict, CodeLockedToBill, "Entry is on a bill")
	case errors.Is(err, ErrOutOfScope):
		h.fail(w, r, http.StatusUnprocessableEntity, CodeActivityOutOfScope, "Activity is outside the paralegal scope")
	case errors.Is(err, ErrInvalid):
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Entry is not valid")
	case errors.Is(err, ErrNotOwner), errors.Is(err, ErrAgentForbidden):
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Not allowed for this entry")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

// authorize answers 503, 401, 403 or 404 itself; it runs before any service call.
func (h *HTTP) authorize(w http.ResponseWriter, r *http.Request, s registry.Subject) (registry.Principal, bool) {
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
	allowed, err := h.Access.Can(r.Context(), p, CapRecord, s)
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

// viewAll reports whether p sees every timekeeper's entries. An error counts as no.
func (h *HTTP) viewAll(ctx context.Context, p registry.Principal) bool {
	ok, err := h.Access.Can(ctx, p, CapViewAll, registry.Subject{})
	return err == nil && ok
}

func actorOf(p registry.Principal) Actor {
	if p.Kind == registry.PrincipalAgent {
		return Actor{UserID: p.ActingFor, Agent: true}
	}
	return Actor{UserID: p.UserID}
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

// listEntries pages the caller's entries, or everyone's with time.view_all.
func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r, registry.Subject{})
	if !ok {
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
	f := EntryFilter{After: EntryID(cur.After), Limit: cur.Limit + 1}
	if !h.viewAll(r.Context(), p) {
		f.UserID = actorOf(p).UserID
	}
	got, err := h.Entries.List(r.Context(), f)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := api.Page[entryView]{Items: []entryView{}}
	for i, e := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: string(got[i-1].ID), Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.Next = next
			break
		}
		page.Items = append(page.Items, entryViewOf(e))
	}
	writeJSON(w, http.StatusOK, page)
}

type recordBody struct {
	MatterID     string `json:"matter_id"`
	WorkDate     string `json:"work_date"`
	Minutes      int    `json:"minutes"`
	ActivityCode string `json:"activity_code,omitempty"`
	Narrative    string `json:"narrative"`
	Billable     *bool  `json:"billable,omitempty"`
}

// create serves POST /time-entries: the caller records their own time.
func (h *HTTP) create(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r, registry.Subject{})
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
	idem.New(h.Idem, func(*http.Request) string { return actorOf(p).UserID }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.record(w, r, p) }),
	).ServeHTTP(w, r)
}

func (h *HTTP) record(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	var b recordBody
	if err := decodeStrict(r, &b); err != nil || !uuidPattern.MatchString(b.MatterID) || b.Minutes <= 0 || b.Narrative == "" {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, defaultProblemTitleBody)
		return
	}
	day, err := time.Parse(time.DateOnly, b.WorkDate)
	if err != nil {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "work_date is not a date")
		return
	}
	// rule: ARC-I-042
	// A matter hidden by a wall answers 404.
	if _, err := h.Access.Can(r.Context(), p, CapRecord, registry.Subject{Kind: matterkind.Subject, ID: b.MatterID}); err != nil {
		h.writeErr(w, r, err)
		return
	}
	by := actorOf(p)
	e := TimeEntry{MatterID: b.MatterID, UserID: by.UserID, WorkDate: day, Minutes: b.Minutes,
		ActivityCode: b.ActivityCode, Narrative: b.Narrative, Billable: b.Billable == nil || *b.Billable}
	var id EntryID
	err = h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = h.Entries.Record(ctx, tx, e, by)
		return err
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	out, err := h.Entries.Get(r.Context(), id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(out.RowVersion))
	writeJSON(w, http.StatusCreated, entryViewOf(out))
}

type editBody struct {
	Minutes      *int    `json:"minutes"`
	Narrative    *string `json:"narrative"`
	ActivityCode *string `json:"activity_code"`
}

// update serves PATCH /time-entries/{time_entry_id} at the If-Match version. An entry on a
// bill answers 409 locked_to_bill.
func (h *HTTP) update(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r, registry.Subject{})
	if !ok {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "time_entry_id")
	if !entryIDPattern.MatchString(id) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "time_entry_id is not valid")
		return
	}
	// rule: ARC-I-006
	if len(r.Header.Values("If-Match")) == 0 {
		h.fail(w, r, http.StatusPreconditionRequired, server.CodePreconditionRequired, "If-Match is required")
		return
	}
	var b editBody
	if err := decodeStrict(r, &b); err != nil || (b.Minutes == nil && b.Narrative == nil && b.ActivityCode == nil) ||
		(b.Minutes != nil && *b.Minutes <= 0) || (b.Narrative != nil && *b.Narrative == "") {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, defaultProblemTitleBody)
		return
	}
	cur, err := h.Entries.Get(ctx, EntryID(id))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	by := actorOf(p)
	// rule: ARC-I-042
	// Without time.view_all another timekeeper's entry does not exist for the caller.
	if cur.UserID != by.UserID && !h.viewAll(ctx, p) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such time entry")
		return
	}
	if _, err := h.Access.Can(ctx, p, CapRecord, registry.Subject{Kind: matterkind.Subject, ID: cur.MatterID}); err != nil {
		h.writeErr(w, r, err)
		return
	}
	if err := server.CheckIfMatch(r, int64(cur.RowVersion)); err != nil {
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	}
	next := cur
	if b.Minutes != nil {
		next.Minutes = *b.Minutes
	}
	if b.Narrative != nil {
		next.Narrative = *b.Narrative
	}
	if b.ActivityCode != nil {
		next.ActivityCode = *b.ActivityCode
	}
	err = h.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error { return h.Entries.Edit(ctx, tx, next, by) })
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	out, err := h.Entries.Get(ctx, EntryID(id))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(out.RowVersion))
	writeJSON(w, http.StatusOK, entryViewOf(out))
}
