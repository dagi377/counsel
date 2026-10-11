package gates

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/audit"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Problem codes the gate routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
)

// ActionViewed is the audit action GET /gates/{gate_id} appends once per view.
const ActionViewed = "gate.viewed"

// Access is the part of identity.Access the gate routes use.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// TxRunner runs fn in one transaction.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// ListFilter selects one page of gates, ordered by id.
type ListFilter struct {
	After gate.GateID
	Limit int
	// States keeps gates in one of these engine states; empty keeps all.
	States []gate.Status
	// Kinds keeps gates of one of these kinds; nil keeps all, empty keeps none.
	Kinds []string
	// NotPreparedBy drops gates this user prepared (or an agent prepared for them).
	NotPreparedBy string
}

// Lister lists gates. The kernel engine has no inbox query yet; boot wires a lister over
// core.gate whose reads run under the caller's RLS, so walled gates never come back.
type Lister interface {
	List(ctx context.Context, f ListFilter) ([]gate.Gate, error)
}

// Reader is the part of the gate engine the read routes use.
type Reader interface {
	Get(ctx context.Context, id gate.GateID) (gate.Gate, error)
}

// HTTP serves GET /gates and GET /gates/{gate_id}. Every dependency is required; a route
// answers 503 and calls nothing while one is missing.
type HTTP struct {
	Gates    Reader
	List     Lister
	Kinds    *Catalog
	Access   Access
	Audit    audit.Log
	DB       TxRunner
	Cursors  api.CursorCodec
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// idPattern is the openapi Id format (uuid).
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Routes mounts the read routes. They write no gate-owned state.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/gates", h.list)
	r.Get("/gates/{gate_id}", h.get)
}

func (h *HTTP) ready() bool {
	return h.Gates != nil && h.List != nil && h.Kinds != nil && h.Access != nil && h.Audit != nil &&
		h.DB != nil && h.Cursors != nil && h.Problems != nil && h.Principal != nil
}

func (h *HTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	pw := h.Problems
	if pw == nil {
		pw = &server.Problems{}
	}
	pw.WriteProblem(w, r, server.NewProblem(status, code, title))
}

// writeErr maps an unexpected error to a problem; the error text never reaches the body.
func (h *HTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, api.ErrBadCursor) {
		h.Problems.WriteProblem(w, r, api.BadCursorProblem())
		return
	}
	h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
}

// approverCaps returns the distinct approver capabilities of every kind, sorted.
func (h *HTTP) approverCaps(ctx context.Context) ([]string, error) {
	kinds, err := h.Kinds.Kinds(ctx)
	if err != nil {
		return nil, err
	}
	var caps []string
	for _, k := range kinds {
		if k.Approver != "" && !slices.Contains(caps, k.Approver) {
			caps = append(caps, k.Approver)
		}
	}
	slices.Sort(caps)
	return caps, nil
}

// heldKinds returns the kinds whose approver capability p holds, ordered by Code.
func (h *HTTP) heldKinds(ctx context.Context, p registry.Principal) ([]string, error) {
	caps, err := h.approverCaps(ctx)
	if err != nil {
		return nil, err
	}
	held := map[string]bool{}
	for _, c := range caps {
		ok, err := h.Access.Can(ctx, p, c, registry.Subject{})
		if err != nil {
			return nil, err
		}
		held[c] = ok
	}
	kinds, err := h.Kinds.Kinds(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, k := range kinds {
		if held[k.Approver] {
			out = append(out, k.Code)
		}
	}
	return out, nil
}

// authorize answers 503, 401 or 403 itself and returns false when the request cannot go on.
// A gate reader holds at least one kind's approver capability. It runs before any gate call.
func (h *HTTP) authorize(w http.ResponseWriter, r *http.Request) (registry.Principal, []string, bool) {
	if !h.ready() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return registry.Principal{}, nil, false
	}
	p, ok := h.Principal(r)
	if !ok {
		h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return registry.Principal{}, nil, false
	}
	// rule: ARC-I-051
	// Fail closed: an error or no approver capability at all stops the request.
	held, err := h.heldKinds(r.Context(), p)
	if err != nil {
		h.writeErr(w, r, err)
		return registry.Principal{}, nil, false
	}
	if len(held) == 0 {
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return registry.Principal{}, nil, false
	}
	return p, held, true
}

// preparerID is the user a principal counts as when preparing: an agent's acting-for user.
func preparerID(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}

// apiStates maps an openapi GateState filter to engine states.
var apiStates = map[string][]gate.Status{
	"open":               {gate.StatusPending},
	"partially_approved": {gate.StatusPending},
	"returned":           {gate.StatusChangesRequested},
	"approved_cooling":   {gate.StatusApprovedCooling},
	"approved":           {gate.StatusApproved},
	"executed":           {gate.StatusApproved},
	"rejected":           {gate.StatusRejected},
	"expired":            {gate.StatusExpired},
	"withdrawn":          {gate.StatusWithdrawn, gate.StatusSuperseded},
}

// gatePage is the openapi listGates body.
type gatePage struct {
	Data       []View  `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

// list serves GET /gates. By default (decidable_by_me=true) it returns only open gates of
// kinds whose approver capability the caller holds and that the caller did not prepare.
func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
	p, held, ok := h.authorize(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	cur, err := api.FromRequest(r, h.Cursors)
	if errors.Is(err, api.ErrBadLimit) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "limit must be an integer")
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	mine := true
	if s := q.Get("decidable_by_me"); s != "" {
		if mine, err = strconv.ParseBool(s); err != nil {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "decidable_by_me must be a boolean")
			return
		}
	}
	f := ListFilter{After: gate.GateID(cur.After), Limit: cur.Limit + 1}
	if s := q.Get("state"); s != "" {
		states, known := apiStates[s]
		if !known {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "state is not valid")
			return
		}
		f.States = states
	}
	if mine {
		// rule: ARC-I-051
		// The inbox shows only kinds the caller may decide, never the caller's own gates.
		f.Kinds = held
		f.NotPreparedBy = preparerID(p)
		if f.States == nil {
			f.States = []gate.Status{gate.StatusPending}
		}
	}
	if k := q.Get("kind"); k != "" {
		if f.Kinds != nil && !slices.Contains(f.Kinds, k) {
			f.Kinds = []string{}
		} else {
			f.Kinds = []string{k}
		}
	}
	got, err := h.List.List(r.Context(), f)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := gatePage{Data: []View{}}
	for i, g := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: string(got[i-1].ID), Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.NextCursor = &next
			break
		}
		page.Data = append(page.Data, h.view(g))
	}
	writeJSON(w, http.StatusOK, page)
}

// get serves GET /gates/{gate_id}. A gate on a walled subject answers 404. Every view
// appends one audit event, in its own transaction, before the body is written.
func (h *HTTP) get(w http.ResponseWriter, r *http.Request) {
	p, held, ok := h.authorize(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "gate_id")
	if !idPattern.MatchString(id) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such gate")
		return
	}
	ctx := r.Context()
	g, err := h.Gates.Get(ctx, gate.GateID(id))
	if errors.Is(err, gate.ErrNotFound) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such gate")
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	k, err := h.Kinds.Spec(g.Kind)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	// rule: ARC-I-042
	// A walled subject is 404, never 403.
	allowed, err := h.Access.Can(ctx, p, k.Approver, registry.Subject{Kind: g.Subject.Kind, ID: g.Subject.ID})
	switch {
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such gate")
		return
	case err != nil:
		h.writeErr(w, r, err)
		return
	}
	// rule: ARC-I-051
	// The kind's approver or the gate's preparer may read it.
	if !(allowed && slices.Contains(held, g.Kind)) && preparerID(g.Preparer) != preparerID(p) {
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return
	}
	ev := audit.Event{ActorKind: string(p.Kind), ActorID: p.UserID, Action: ActionViewed,
		Subject: registry.Subject{Kind: "gate", ID: string(g.ID)}}
	if p.Kind == registry.PrincipalAgent {
		ev.ActorID, ev.ActingFor = p.RunID, p.ActingFor
	}
	if err := h.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error { return h.Audit.Append(ctx, tx, ev) }); err != nil {
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(g.Version))
	writeJSON(w, http.StatusOK, h.view(g))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Actor is the openapi Actor.
type Actor struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	ActingFor string `json:"acting_for,omitempty"`
}

// View is a gate as the API returns it (openapi Gate).
type View struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Version      int             `json:"version"`
	State        string          `json:"state"`
	Risk         string          `json:"risk,omitempty"`
	Capability   string          `json:"capability,omitempty"`
	Subject      map[string]any  `json:"subject"`
	Payload      json.RawMessage `json:"payload"`
	PayloadHash  string          `json:"payload_hash"`
	Preparers    []Actor         `json:"preparers"`
	MinApprovers int             `json:"min_approvers"`
	OpenedAt     string          `json:"opened_at"`
	ExpiresAt    string          `json:"expires_at"`
	EffectAfter  *string         `json:"effect_after"`
	Decisions    []any           `json:"decisions"`
}

// state maps an engine state to the openapi GateState.
func state(g gate.Gate) string {
	switch g.Status {
	case gate.StatusPending:
		if len(g.Approvers) > 0 {
			return "partially_approved"
		}
		return "open"
	case gate.StatusApprovedCooling:
		return "approved_cooling"
	case gate.StatusApproved:
		if g.EffectRan {
			return "executed"
		}
		return "approved"
	case gate.StatusRejected:
		return "rejected"
	case gate.StatusChangesRequested:
		return "returned"
	case gate.StatusExpired:
		return "expired"
	default: // withdrawn, superseded
		return "withdrawn"
	}
}

// view maps g to the openapi Gate. Decisions stay empty: the engine does not expose
// per-decision records yet.
func (h *HTTP) view(g gate.Gate) View {
	v := View{ID: string(g.ID), Kind: g.Kind, Version: g.Version, State: state(g),
		Subject: map[string]any{"type": g.Subject.Kind, "id": g.Subject.ID}, Payload: g.Payload,
		PayloadHash: g.PayloadHash, MinApprovers: 1,
		OpenedAt:  g.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: g.ExpiresAt.UTC().Format(time.RFC3339),
		Decisions: []any{}}
	if k, err := h.Kinds.Spec(g.Kind); err == nil {
		v.Risk, v.Capability = string(k.Risk), k.Approver
		if k.MinApprovers > 0 {
			v.MinApprovers = k.MinApprovers
		}
	}
	prep := Actor{Type: "user", ID: g.Preparer.UserID}
	if g.Preparer.Kind == registry.PrincipalAgent {
		prep = Actor{Type: "agent", ID: g.Preparer.RunID, ActingFor: g.Preparer.ActingFor}
	}
	v.Preparers = []Actor{prep}
	if !g.EffectiveAt.IsZero() {
		s := g.EffectiveAt.UTC().Format(time.RFC3339)
		v.EffectAfter = &s
	}
	if len(v.Payload) == 0 {
		v.Payload = json.RawMessage(`{}`)
	}
	return v
}
