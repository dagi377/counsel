package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/ids"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapIntegrations manages webhook endpoints.
const CapIntegrations = "platform.integrations"

// GateIntegrationScope is the gate kind that adds an endpoint or widens what it receives
// (ARC-I-049). Its effect, never the route, creates the endpoint and its signing secret.
const GateIntegrationScope = "platform.integration_scope"

// SubjectEndpoint is the subject kind of a webhook endpoint.
const SubjectEndpoint = "platform.webhook"

// Problem codes the endpoint routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
	// CodeWebhookExpired: the endpoint has expired; create a new one (proposed).
	CodeWebhookExpired = "webhook_expired"
)

// ErrEndpointExpired: the endpoint has expired and cannot be changed or renewed.
var ErrEndpointExpired = errors.New("webhook: endpoint expired")

// ErrStaleVersion: the endpoint changed since the version the caller read.
var ErrStaleVersion = errors.New("webhook: stale endpoint version")

// VersionedExpiry is an ExpiryStore that renews only while the endpoint is at the version
// read (PGExpiry). The PATCH route uses it when the Expiry port has it.
type VersionedExpiry interface {
	SetExpiryAt(ctx context.Context, id string, expiresAt *time.Time, version int) error
}

// Endpoint statuses (platform.webhook.status).
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusExpired  = "expired"
)

// EndpointInfo is an endpoint as the routes show it. It never carries the signing secret.
type EndpointInfo struct {
	ID        string
	URL       string
	Events    []string // empty means all registered types
	Status    string
	ExpiresAt *time.Time
	Version   int
}

// EndpointFilter selects one page: endpoints whose id sorts after After, at most Limit.
type EndpointFilter struct {
	After string
	Limit int
}

// EndpointReads lists and reads endpoints (Dispatcher has no list). It never selects the
// secret reference.
type EndpointReads interface {
	Endpoints(ctx context.Context, f EndpointFilter) ([]EndpointInfo, error)
	// Endpoint returns ErrUnknownEndpoint for an unknown id.
	Endpoint(ctx context.Context, id string) (EndpointInfo, error)
}

// Access is the part of identity.Access the routes use.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// Gates is the part of the gate engine the routes use.
type Gates interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error)
	Get(ctx context.Context, id gate.GateID) (gate.Gate, error)
}

// TxRunner runs fn in one transaction; kernel/db's pool implements it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// HTTP serves GET /webhook-endpoints, POST /webhook-endpoints and
// PATCH /webhook-endpoints/{webhook_id}. Every dependency is required; a route answers 503
// and calls nothing while one is missing.
type HTTP struct {
	Endpoints EndpointReads
	Expiry    ExpiryStore
	Access    Access
	DB        TxRunner
	Gates     Gates
	Idem      idem.Store
	Cursors   api.CursorCodec
	Problems  server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
	// NewID mints the id of an endpoint to be created; nil means ids.New.
	NewID func() string
	// MinApprovers is the platform.integration_scope approver count for the Gate body; 0 means 1.
	MinApprovers int
}

var (
	endpointIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	eventNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_.]*$`)
)

// maxEvents bounds the event types one endpoint names.
const maxEvents = 100

// Routes mounts the endpoint routes.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/webhook-endpoints", h.list)
	// rule: ARC-I-012
	// Creating an endpoint and widening its events are gate-owned
	// (webhook_endpoint.events, webhook_endpoint.active): those routes open the gate. Only the
	// expiry is written directly.
	r.Method(http.MethodPost, "/webhook-endpoints", server.Declare(h.create))
	r.Method(http.MethodPatch, "/webhook-endpoints/{webhook_id}", server.Declare(h.patch, "webhook_endpoint.expires_at"))
}

func (h *HTTP) ready() bool {
	return h.Endpoints != nil && h.Expiry != nil && h.Access != nil && h.DB != nil && h.Gates != nil &&
		h.Idem != nil && h.Cursors != nil && h.Problems != nil && h.Principal != nil
}

func (h *HTTP) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
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

// writeErr maps a port error to a problem. Error text (which may name a URL) never reaches
// the body.
func (h *HTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, api.ErrBadCursor):
		h.problems().WriteProblem(w, r, api.BadCursorProblem())
	case errors.Is(err, ErrUnknownEndpoint), errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such webhook endpoint")
	case errors.Is(err, ErrEndpointExpired):
		h.fail(w, r, http.StatusConflict, CodeWebhookExpired, "The endpoint has expired; create a new one")
	case errors.Is(err, ErrStaleVersion):
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
	case errors.Is(err, ErrPastExpiry):
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "expires_at must be in the future")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

// authorize answers 503, 401, 403 or 404 itself; it runs before any port or gate call.
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
	allowed, err := h.Access.Can(r.Context(), p, CapIntegrations, s)
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

// withIdem requires an Idempotency-Key before any port or gate call and replays a repeat.
func (h *HTTP) withIdem(w http.ResponseWriter, r *http.Request, p registry.Principal, next http.HandlerFunc) {
	// rule: ARC-I-007
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return
	}
	// rule: ARC-I-008
	idem.New(h.Idem, func(*http.Request) string { return actorOf(p) }, idem.DefaultTTL)(next).ServeHTTP(w, r)
}

// endpointView is the endpoint body. It has no secret field: the signing secret never
// appears in a response.
type endpointView struct {
	ID        string   `json:"id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Status    string   `json:"status"`
	ExpiresAt *string  `json:"expires_at"`
	Version   int      `json:"version"`
}

func viewOf(e EndpointInfo) endpointView {
	v := endpointView{ID: e.ID, URL: e.URL, Events: e.Events, Status: e.Status, Version: e.Version}
	if v.Events == nil {
		v.Events = []string{}
	}
	if e.ExpiresAt != nil {
		s := e.ExpiresAt.UTC().Format(time.RFC3339)
		v.ExpiresAt = &s
	}
	return v
}

// list serves GET /webhook-endpoints, one page at a time in id order.
func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r, registry.Subject{}); !ok {
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
	got, err := h.Endpoints.Endpoints(r.Context(), EndpointFilter{After: cur.After, Limit: cur.Limit + 1})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := api.Page[endpointView]{Items: []endpointView{}}
	for i, e := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: got[i-1].ID, Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.Next = next
			break
		}
		page.Items = append(page.Items, viewOf(e))
	}
	writeJSON(w, http.StatusOK, page)
}

// createBody is the POST /webhook-endpoints body. DataLeavesCanada defaults to true, so the
// approver sees the wider case unless the requester states otherwise.
type createBody struct {
	URL              string   `json:"url"`
	Events           []string `json:"events"`
	ExpiresAt        *string  `json:"expires_at,omitempty"`
	DataLeavesCanada *bool    `json:"data_leaves_canada,omitempty"`
}

func validEvents(evs []string) bool {
	if len(evs) == 0 || len(evs) > maxEvents {
		return false
	}
	for _, e := range evs {
		if !eventNamePattern.MatchString(e) {
			return false
		}
	}
	return true
}

func parseExpiry(s *string) (*time.Time, bool) {
	if s == nil {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil, false
	}
	return &t, true
}

// create serves POST /webhook-endpoints: it opens gate platform.integration_scope and answers
// 202 with the Gate. The endpoint and its secret exist only once the gate's effect runs.
func (h *HTTP) create(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r, registry.Subject{})
	if !ok {
		return
	}
	h.withIdem(w, r, p, func(w http.ResponseWriter, r *http.Request) {
		var b createBody
		if err := decodeStrict(r, &b); err != nil || !strings.HasPrefix(b.URL, "https://") || len(b.URL) > 2048 ||
			!validEvents(b.Events) {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
			return
		}
		exp, ok := parseExpiry(b.ExpiresAt)
		if !ok {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "expires_at is not an RFC 3339 time")
			return
		}
		if exp != nil && !exp.After(h.now()) {
			h.writeErr(w, r, ErrPastExpiry)
			return
		}
		leaves := true
		if b.DataLeavesCanada != nil {
			leaves = *b.DataLeavesCanada
		}
		id := h.newID()
		h.openScopeGate(w, r, p, id, map[string]any{
			"integration_id": id, "scopes": b.Events, "data_leaves_canada": leaves,
			"change": map[string]any{"action": "create", "url": b.URL, "expires_at": b.ExpiresAt},
		})
	})
}

// patchBody is the merge-patch body: expires_at (null = never expires) and/or events.
type patchBody struct {
	ExpiresAt json.RawMessage `json:"expires_at,omitempty"`
	Events    *[]string       `json:"events,omitempty"`
}

// patch serves PATCH /webhook-endpoints/{webhook_id}. An expiry-only change applies now (200).
// Any change to the events opens gate platform.integration_scope (202) carrying the whole
// patch, and changes nothing until the gate's effect runs.
func (h *HTTP) patch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "webhook_id")
	subject := registry.Subject{}
	if endpointIDPattern.MatchString(id) {
		subject = registry.Subject{Kind: SubjectEndpoint, ID: strings.ToLower(id)}
	}
	p, ok := h.authorize(w, r, subject)
	if !ok {
		return
	}
	h.withIdem(w, r, p, func(w http.ResponseWriter, r *http.Request) { h.applyPatch(w, r, p, strings.ToLower(id)) })
}

func (h *HTTP) applyPatch(w http.ResponseWriter, r *http.Request, p registry.Principal, id string) {
	var b patchBody
	if !endpointIDPattern.MatchString(id) || decodeStrict(r, &b) != nil || (b.ExpiresAt == nil && b.Events == nil) ||
		(b.Events != nil && !validEvents(*b.Events)) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	var expStr *string
	if b.ExpiresAt != nil && string(b.ExpiresAt) != "null" {
		var s string
		if json.Unmarshal(b.ExpiresAt, &s) != nil {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "expires_at is not valid")
			return
		}
		expStr = &s
	}
	exp, ok := parseExpiry(expStr)
	if !ok {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "expires_at is not an RFC 3339 time")
		return
	}
	// rule: ARC-I-006
	// A missing If-Match is refused before any port or gate call.
	if strings.TrimSpace(r.Header.Get("If-Match")) == "" {
		h.fail(w, r, http.StatusPreconditionRequired, server.CodePreconditionRequired, "If-Match is required")
		return
	}
	ctx := r.Context()
	cur, err := h.Endpoints.Endpoint(ctx, id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if err := server.CheckIfMatch(r, int64(cur.Version)); err != nil {
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	}
	if cur.Status == StatusExpired {
		h.writeErr(w, r, ErrEndpointExpired)
		return
	}
	if b.Events != nil && !sameEvents(*b.Events, cur.Events) {
		if exp != nil && !exp.After(h.now()) {
			h.writeErr(w, r, ErrPastExpiry)
			return
		}
		// rule: ARC-I-049
		change := map[string]any{"action": "update", "events_from": cur.Events, "base_version": cur.Version}
		if b.ExpiresAt != nil {
			change["expires_at"] = expStr
		}
		h.openScopeGate(w, r, p, id, map[string]any{
			"integration_id": id, "scopes": *b.Events, "data_leaves_canada": true, "change": change,
		})
		return
	}
	if b.ExpiresAt == nil {
		// The events are unchanged and nothing else was asked: answer the endpoint as it is.
		server.SetETag(w, int64(cur.Version))
		writeJSON(w, http.StatusOK, viewOf(cur))
		return
	}
	if err := h.renew(ctx, id, exp, cur.Version); err != nil {
		h.writeErr(w, r, err)
		return
	}
	out, err := h.Endpoints.Endpoint(ctx, id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(out.Version))
	writeJSON(w, http.StatusOK, viewOf(out))
}

// renew sets the expiry. With a VersionedExpiry the write holds only at version, so a change
// made after the If-Match check is ErrStaleVersion (412), never overwritten.
func (h *HTTP) renew(ctx context.Context, id string, exp *time.Time, version int) error {
	v, ok := h.Expiry.(VersionedExpiry)
	if !ok {
		return RenewEndpoint(ctx, h.Expiry, id, exp, h.now())
	}
	// rule: ARC-I-049
	if exp != nil && !exp.After(h.now()) {
		return fmt.Errorf("renew endpoint %s: %w", id, ErrPastExpiry)
	}
	// rule: ARC-I-006
	if err := v.SetExpiryAt(ctx, id, exp, version); err != nil {
		return fmt.Errorf("renew endpoint %s: %w", id, err)
	}
	return nil
}

func sameEvents(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(slices.Compact(x), slices.Compact(y))
}

func (h *HTTP) openScopeGate(w http.ResponseWriter, r *http.Request, p registry.Principal, id string, payload map[string]any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	var gid gate.GateID
	err = h.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		gid, err = h.Gates.Open(ctx, tx, GateIntegrationScope, gate.Subject{Kind: SubjectEndpoint, ID: id}, raw, p)
		return err
	})
	if err != nil {
		h.writeErr(w, r, fmt.Errorf("open gate: %w", err))
		return
	}
	g, err := h.Gates.Get(ctx, gid)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, h.gateBody(g))
}

func (h *HTTP) newID() string {
	if h.NewID != nil {
		return h.NewID()
	}
	return string(ids.New())
}

// actorOf is the user a request acts for: the user, or the user an agent acts for.
func actorOf(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
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

type actorBody struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	ActingFor string `json:"acting_for,omitempty"`
}

type gateBody struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Version      int             `json:"version"`
	State        string          `json:"state"`
	Subject      map[string]any  `json:"subject"`
	Payload      json.RawMessage `json:"payload"`
	PayloadHash  string          `json:"payload_hash"`
	Preparers    []actorBody     `json:"preparers"`
	MinApprovers int             `json:"min_approvers"`
	OpenedAt     string          `json:"opened_at"`
	ExpiresAt    string          `json:"expires_at"`
	Decisions    []any           `json:"decisions"`
}

func (h *HTTP) gateBody(g gate.Gate) gateBody {
	min := h.MinApprovers
	if min == 0 {
		min = 1
	}
	prep := actorBody{Type: string(g.Preparer.Kind), ID: g.Preparer.UserID}
	if g.Preparer.Kind == registry.PrincipalAgent {
		prep = actorBody{Type: "agent", ID: g.Preparer.RunID, ActingFor: g.Preparer.ActingFor}
	}
	return gateBody{
		ID: string(g.ID), Kind: g.Kind, Version: g.Version, State: gateStates[g.Status],
		Subject: map[string]any{"type": g.Subject.Kind, "id": g.Subject.ID}, Payload: g.Payload,
		PayloadHash: g.PayloadHash, Preparers: []actorBody{prep}, MinApprovers: min,
		OpenedAt:  g.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: g.ExpiresAt.UTC().Format(time.RFC3339),
		Decisions: []any{},
	}
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
