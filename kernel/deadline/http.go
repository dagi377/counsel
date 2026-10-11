package deadline

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/server"
)

// Problem codes the deadline routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
	CodeUnknownRule       = "unknown_rule"
)

// HTTPConfig names the app's capabilities, subject kinds and confirmation gate. The kernel
// knows none of them; the app supplies all of them at registration.
type HTTPConfig struct {
	// CapView reads deadlines; CapEdit proposes them.
	CapView, CapEdit string
	// SubjectKind is the kind of the business object a deadline belongs to. SubjectKey is
	// that object's id field in the propose body, the list query and the gate payload.
	SubjectKind, SubjectKey string
	// DeadlineKind is the access and gate subject kind of a stored deadline.
	DeadlineKind string
	// ConfirmGate is the gate kind whose effect confirms a proposed deadline.
	ConfirmGate string
}

func (c HTTPConfig) valid() bool {
	return c.CapView != "" && c.CapEdit != "" && c.SubjectKind != "" && c.SubjectKey != "" &&
		c.DeadlineKind != "" && c.ConfirmGate != ""
}

// ListFilter selects one page of deadlines, ordered by id. Zero fields do not filter.
type ListFilter struct {
	After   DeadlineID // return deadlines with id greater than this
	Limit   int
	Subject Subject
	State   State
}

// PageLister lists stored deadlines one page at a time (HTTP-22; Engine has no List).
// Its method is ListPage so a store can implement it beside Lister. The Postgres store
// implements it in a follow-up; tests use a fake.
type PageLister interface {
	ListPage(ctx context.Context, f ListFilter) ([]Deadline, error)
}

// Gates is the part of the gate engine the propose route uses.
type Gates interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error)
	Get(ctx context.Context, id gate.GateID) (gate.Gate, error)
}

// Access is the part of identity.Access the routes use.
type Access interface {
	Can(ctx context.Context, p Principal, capability string, s Subject) (bool, error)
}

// HTTP serves GET /deadlines, POST /deadlines and GET /deadlines/{deadline_id}. Propose
// computes and saves a proposed deadline and opens Config.ConfirmGate; it never confirms.
// Every dependency is required; a route answers 503 and calls nothing while one is missing.
type HTTP struct {
	Config   HTTPConfig
	Engine   Engine
	Lister   PageLister
	Gates    Gates
	DB       TxRunner
	Access   Access
	Cursors  api.CursorCodec
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (Principal, bool)
}

// idPattern is the openapi Id format (uuid).
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Routes mounts the deadline routes.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/deadlines", h.list)
	// rule: ARC-I-012
	// Confirmation is gate-owned: propose saves state proposed and opens the gate only.
	r.Method(http.MethodPost, "/deadlines", server.Declare(h.propose, "deadline.status=proposed"))
	r.Get("/deadlines/{deadline_id}", h.get)
}

func (h *HTTP) ready() bool {
	return h.Config.valid() && h.Engine != nil && h.Lister != nil && h.Gates != nil && h.DB != nil &&
		h.Access != nil && h.Cursors != nil && h.Idem != nil && h.Problems != nil && h.Principal != nil
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
	case errors.Is(err, ErrNotFound):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such deadline")
	case errors.Is(err, ErrUnknownRule), errors.Is(err, ErrUnknownPack):
		h.fail(w, r, http.StatusUnprocessableEntity, CodeUnknownRule, "Unknown deadline rule")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

// authorize answers 503, 401, 403 or 404 itself and returns false when the request cannot go
// on. It runs before any service call.
func (h *HTTP) authorize(w http.ResponseWriter, r *http.Request, capability string, s Subject) (Principal, bool) {
	if !h.ready() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return Principal{}, false
	}
	p, ok := h.Principal(r)
	if !ok {
		h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return Principal{}, false
	}
	// rule: ARC-I-051
	// Fail closed: an error or a missing capability stops the request.
	allowed, err := h.Access.Can(r.Context(), p, capability, s)
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such deadline")
		return Principal{}, false
	case err != nil:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return Principal{}, false
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return Principal{}, false
	}
	return p, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// view renders a deadline; the subject id goes under Config.SubjectKey.
func (h *HTTP) view(d Deadline) map[string]any {
	p := d.Proposed
	return map[string]any{
		"id": string(d.ID), h.Config.SubjectKey: p.Subject.ID, "state": string(d.State),
		"pack_id": p.PackID, "pack_version": p.PackVersion, "rule_id": p.RuleID,
		"trigger_date": p.Trigger.String(), "due_on": p.Due.String(),
	}
}

// list serves GET /deadlines: deadlines by id, one page at a time.
func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sid := q.Get(h.Config.SubjectKey)
	subject := Subject{}
	if idPattern.MatchString(sid) {
		subject = Subject{Kind: h.Config.SubjectKind, ID: sid}
	}
	if _, ok := h.authorize(w, r, h.Config.CapView, subject); !ok {
		return
	}
	state := State(q.Get("state"))
	if (sid != "" && subject.ID == "") || (state != "" && state != StateProposed && state != StateConfirmed) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Query is not valid")
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
	got, err := h.Lister.ListPage(r.Context(), ListFilter{After: DeadlineID(cur.After), Limit: cur.Limit + 1,
		Subject: subject, State: state})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	data := []map[string]any{}
	var next *string
	for i, d := range got {
		if i == cur.Limit {
			s, err := h.Cursors.Encode(api.Cursor{After: string(got[i-1].ID), Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			next = &s
			break
		}
		data = append(data, h.view(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "next_cursor": next})
}

// proposeBody is the POST /deadlines body; the subject id arrives under Config.SubjectKey.
type proposeBody struct {
	SubjectID   string
	Title       string
	PackID      string
	RuleID      string
	TriggerDate clock.Date
}

// parsePropose reads the body strictly: unknown fields, a malformed id or a bad date fail.
func (h *HTTP) parsePropose(raw []byte) (proposeBody, bool) {
	var m map[string]json.RawMessage
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&m); err != nil || dec.More() {
		return proposeBody{}, false
	}
	fields := map[string]*string{}
	var b proposeBody
	var trigger string
	fields[h.Config.SubjectKey] = &b.SubjectID
	fields["title"], fields["pack_id"], fields["rule_id"], fields["trigger_date"] = &b.Title, &b.PackID, &b.RuleID, &trigger
	for k, v := range m {
		dst, ok := fields[k]
		if !ok || json.Unmarshal(v, dst) != nil {
			return proposeBody{}, false
		}
	}
	t, err := time.Parse(time.DateOnly, trigger)
	if err != nil {
		return proposeBody{}, false
	}
	b.TriggerDate = clock.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
	ok := idPattern.MatchString(b.SubjectID) && strings.TrimSpace(b.Title) != "" && len(b.Title) <= 200 &&
		b.PackID != "" && b.RuleID != ""
	return b, ok
}

// propose serves POST /deadlines: compute one deadline, save it proposed and open the
// confirmation gate in one transaction. It never calls Engine.Confirm.
func (h *HTTP) propose(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r, h.Config.CapEdit, Subject{})
	if !ok {
		return
	}
	// rule: ARC-I-007
	// The key is required before any service or gate call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return
	}
	// rule: ARC-I-008
	idem.New(h.Idem, func(*http.Request) string { return p.UserID + p.RunID }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.save(w, r) }),
	).ServeHTTP(w, r)
}

func (h *HTTP) save(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	b, ok := h.parsePropose(raw)
	if !ok {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	subject := Subject{Kind: h.Config.SubjectKind, ID: b.SubjectID}
	// The parent object decides visibility: a walled one answers 404, never 403.
	pr, ok := h.authorize(w, r, h.Config.CapEdit, subject)
	if !ok {
		return
	}
	proposals, err := h.Engine.Compute(r.Context(), b.PackID, b.RuleID, b.TriggerDate, subject)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if len(proposals) != 1 {
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "The rule must yield exactly one deadline")
		return
	}
	prop := proposals[0]
	payload, err := json.Marshal(map[string]any{
		h.Config.SubjectKey: b.SubjectID, "title": b.Title, "due_on": prop.Due.String(),
		"rule_id": prop.RuleID, "trigger_date": prop.Trigger.String(),
		"source": map[string]string{"pack_id": prop.PackID, "pack_version": prop.PackVersion},
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	// rule: ARC-I-012
	var gid gate.GateID
	err = h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		id, err := h.Engine.Save(ctx, tx, prop)
		if err != nil {
			return err
		}
		gid, err = h.Gates.Open(ctx, tx, h.Config.ConfirmGate, gate.Subject{Kind: h.Config.DeadlineKind, ID: string(id)}, payload, pr)
		return err
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	g, err := h.Gates.Get(r.Context(), gid)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, h.gateBody(g, b.SubjectID))
}

// gateStates maps engine states to the openapi GateState.
var gateStates = map[gate.Status]string{
	gate.StatusPending:          "open",
	gate.StatusApprovedCooling:  "approved_cooling",
	gate.StatusApproved:         "approved",
	gate.StatusRejected:         "rejected",
	gate.StatusChangesRequested: "returned",
	gate.StatusWithdrawn:        "withdrawn",
	gate.StatusExpired:          "expired",
	gate.StatusSuperseded:       "withdrawn",
}

// gateBody renders an opened gate as the openapi Gate; the parent id goes under
// Config.SubjectKey. The confirmation gate needs one approver.
func (h *HTTP) gateBody(g gate.Gate, parentID string) map[string]any {
	prep := map[string]any{"type": string(g.Preparer.Kind), "id": g.Preparer.UserID}
	if g.Preparer.RunID != "" {
		prep = map[string]any{"type": "agent", "id": g.Preparer.RunID, "acting_for": g.Preparer.ActingFor}
	}
	subject := map[string]any{"type": g.Subject.Kind, "id": g.Subject.ID}
	if parentID != "" {
		subject[h.Config.SubjectKey] = parentID
	}
	return map[string]any{
		"id": string(g.ID), "kind": g.Kind, "version": g.Version, "state": gateStates[g.Status],
		"subject": subject, "payload": g.Payload, "payload_hash": g.PayloadHash,
		"preparers": []any{prep}, "min_approvers": 1,
		"opened_at":  g.CreatedAt.UTC().Format(time.RFC3339),
		"expires_at": g.ExpiresAt.UTC().Format(time.RFC3339),
		"decisions":  []any{},
	}
}

// get serves GET /deadlines/{deadline_id}. A deadline hidden by a wall or RLS answers 404.
func (h *HTTP) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "deadline_id")
	if !idPattern.MatchString(id) {
		if _, ok := h.authorize(w, r, h.Config.CapView, Subject{}); !ok {
			return
		}
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "deadline_id is not valid")
		return
	}
	if _, ok := h.authorize(w, r, h.Config.CapView, Subject{Kind: h.Config.DeadlineKind, ID: id}); !ok {
		return
	}
	d, err := h.Engine.Get(r.Context(), DeadlineID(id))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h.view(d))
}
