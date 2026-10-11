// Package access serves the firm administration routes: the user list, role changes, the
// role catalogue and the audit-event query. A role change only proposes: it opens gate
// core.role_change, and the gate's effect, never the route, assigns the role.
package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/api"
	kaudit "github.com/dagi377/counsel/kernel/audit"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Capabilities the admin routes check (spec/legal/access.json).
const (
	// CapUsers lists users.
	CapUsers = "admin.users"
	// CapRoles reads the role catalogue and proposes role changes.
	CapRoles = "admin.roles"
	// CapAuditLog reads the audit log.
	CapAuditLog = "admin.audit_log"
)

// GateRoleChange is the gate kind PUT /users/{user_id}/roles opens. Its effect assigns the
// role; the route assigns nothing.
const GateRoleChange = "core.role_change"

// SubjectUser is the subject kind of a firm user (gate subject and access checks).
const SubjectUser = "core.user"

// Problem codes the admin routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
	// CodeLastAdmin: the change would leave the firm with no active admin (proposed).
	CodeLastAdmin = "last_admin"
	// CodeUnknownRole: the requested role does not exist.
	CodeUnknownRole = "unknown_role"
)

// Sentinel errors the read ports return.
var (
	ErrNotFound = errors.New("access: not found")
	// ErrLastAdmin: the role change would remove the last active admin.
	ErrLastAdmin = errors.New("access: last admin")
	// ErrUnknownRole: no such role.
	ErrUnknownRole = errors.New("access: unknown role")
)

// User is one firm user as the admin routes show it. Version is the row version If-Match
// is checked against.
type User struct {
	ID          string
	Email       string
	DisplayName string
	RoleID      string
	Active      bool
	Version     int
}

// Role is one firm role with the capability ids it grants.
type Role struct {
	ID           string
	Key          string
	Name         string
	Capabilities []string
}

// Filter selects one page: rows whose id (or audit sequence) sorts after After, at most Limit.
type Filter struct {
	After string
	Limit int
}

// Directory is the narrow user read port (signatory.Directory has no list). Reads run under
// the caller's RLS session.
type Directory interface {
	Users(ctx context.Context, f Filter) ([]User, error)
	// User returns ErrNotFound for an unknown or hidden id.
	User(ctx context.Context, id string) (User, error)
	// CheckRoleChange refuses a change that may not be proposed: ErrUnknownRole, or
	// ErrLastAdmin when it would leave no active admin. It changes nothing.
	CheckRoleChange(ctx context.Context, userID, roleID string) error
}

// RoleCatalog lists the firm's roles.
type RoleCatalog interface {
	Roles(ctx context.Context) ([]Role, error)
}

// AuditQuery reads the audit log in sequence order (audit.Log has Append and Verify only).
// Filter.After is a decimal sequence number.
type AuditQuery interface {
	Events(ctx context.Context, f Filter) ([]kaudit.Event, error)
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

// HTTP serves GET /users, PUT /users/{user_id}/roles, GET /roles and GET /audit-events.
// Every dependency is required; a route answers 503 and calls nothing while one is missing.
type HTTP struct {
	Users    Directory
	Roles    RoleCatalog
	Audit    AuditQuery
	Masker   kaudit.Masker
	Access   Access
	DB       TxRunner
	Gates    Gates
	Idem     idem.Store
	Cursors  api.CursorCodec
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
	// MinApprovers is the core.role_change approver count for the Gate body; 0 means 1.
	MinApprovers int
}

// idPattern is the openapi Id format (uuid).
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Routes mounts the admin routes.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/users", h.listUsers)
	// rule: ARC-I-012
	// Assigned roles are gate-owned (user.roles -> core.role_change): the route opens the
	// gate and declares no write of its own.
	r.Method(http.MethodPut, "/users/{user_id}/roles", server.Declare(h.changeRoles))
	r.Get("/roles", h.listRoles)
	r.Get("/audit-events", h.listAudit)
}

func (h *HTTP) ready() bool {
	return h.Users != nil && h.Roles != nil && h.Audit != nil && h.Masker != nil && h.Access != nil &&
		h.DB != nil && h.Gates != nil && h.Idem != nil && h.Cursors != nil && h.Problems != nil && h.Principal != nil
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

// writeErr maps a port error to a problem. Error text never reaches the body.
func (h *HTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, api.ErrBadCursor):
		h.problems().WriteProblem(w, r, api.BadCursorProblem())
	case errors.Is(err, ErrNotFound), errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
	case errors.Is(err, ErrLastAdmin):
		h.fail(w, r, http.StatusConflict, CodeLastAdmin, "The firm must keep at least one active admin")
	case errors.Is(err, ErrUnknownRole):
		h.fail(w, r, http.StatusUnprocessableEntity, CodeUnknownRole, "No such role")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

// authorize answers 503, 401, 403 or 404 itself; it runs before any port or gate call.
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

// pageCursor reads cursor and limit; it answers 400 itself.
func (h *HTTP) pageCursor(w http.ResponseWriter, r *http.Request) (api.Cursor, bool) {
	cur, err := api.FromRequest(r, h.Cursors)
	if errors.Is(err, api.ErrBadLimit) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "limit must be an integer")
		return api.Cursor{}, false
	}
	if err != nil {
		h.writeErr(w, r, err)
		return api.Cursor{}, false
	}
	return cur, true
}

// writePage writes rows (fetched with one extra) as one page; key gives a row's cursor key.
func writePage[T, V any](h *HTTP, w http.ResponseWriter, r *http.Request, cur api.Cursor, rows []T, key func(T) string, view func(T) V) {
	page := api.Page[V]{Items: []V{}}
	for i, row := range rows {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: key(rows[i-1]), Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.Next = next
			break
		}
		page.Items = append(page.Items, view(row))
	}
	writeJSON(w, http.StatusOK, page)
}

type userView struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	RoleID      string `json:"role_id"`
	Active      bool   `json:"active"`
	Version     int    `json:"version"`
}

func userViewOf(u User) userView {
	return userView{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, RoleID: u.RoleID, Active: u.Active, Version: u.Version}
}

// listUsers serves GET /users. RLS limits the rows the directory returns.
func (h *HTTP) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r, CapUsers, registry.Subject{}); !ok {
		return
	}
	cur, ok := h.pageCursor(w, r)
	if !ok {
		return
	}
	// One extra row tells whether another page follows.
	got, err := h.Users.Users(r.Context(), Filter{After: cur.After, Limit: cur.Limit + 1})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writePage(h, w, r, cur, got, func(u User) string { return u.ID }, userViewOf)
}

type roleView struct {
	ID           string   `json:"id"`
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

// listRoles serves GET /roles: every role with its capability ids.
func (h *HTTP) listRoles(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r, CapRoles, registry.Subject{}); !ok {
		return
	}
	got, err := h.Roles.Roles(r.Context())
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	out := make([]roleView, 0, len(got))
	for _, ro := range got {
		caps := ro.Capabilities
		if caps == nil {
			caps = []string{}
		}
		out = append(out, roleView{ID: ro.ID, Key: ro.Key, Name: ro.Name, Capabilities: caps})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

type auditView struct {
	Seq       int64             `json:"seq"`
	At        string            `json:"at"`
	ActorKind string            `json:"actor_kind"`
	ActorID   string            `json:"actor_id"`
	ActingFor string            `json:"acting_for,omitempty"`
	Action    string            `json:"action"`
	Subject   map[string]string `json:"subject,omitempty"`
	Detail    map[string]string `json:"detail"`
}

// listAudit serves GET /audit-events: read-only, in sequence order, detail masked.
func (h *HTTP) listAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r, CapAuditLog, registry.Subject{}); !ok {
		return
	}
	cur, ok := h.pageCursor(w, r)
	if !ok {
		return
	}
	got, err := h.Audit.Events(r.Context(), Filter{After: cur.After, Limit: cur.Limit + 1})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writePage(h, w, r, cur, got, func(e kaudit.Event) string { return strconv.FormatInt(int64(e.Seq), 10) },
		func(e kaudit.Event) auditView {
			// rule: ARC-I-044
			// Detail leaves only through the masker, even though it was masked when stored.
			detail := h.Masker.Mask(e.Detail)
			if detail == nil {
				detail = map[string]string{}
			}
			v := auditView{Seq: int64(e.Seq), At: e.At.UTC().Format(time.RFC3339), ActorKind: e.ActorKind,
				ActorID: e.ActorID, ActingFor: e.ActingFor, Action: e.Action, Detail: detail}
			if e.Subject.Kind != "" {
				v.Subject = map[string]string{"kind": e.Subject.Kind, "id": e.Subject.ID}
			}
			return v
		})
}

// roleChangeBody is the PUT /users/{user_id}/roles body: the one role the user should hold
// (core.app_user.role_id).
type roleChangeBody struct {
	RoleID string `json:"role_id"`
}

// changeRoles serves PUT /users/{user_id}/roles: it opens gate core.role_change and answers
// 202 with the Gate. The user's assigned role is unchanged until the gate's effect runs.
func (h *HTTP) changeRoles(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	subject := registry.Subject{}
	if idPattern.MatchString(userID) {
		subject = registry.Subject{Kind: SubjectUser, ID: userID}
	}
	p, ok := h.authorize(w, r, CapRoles, subject)
	if !ok {
		return
	}
	// rule: ARC-I-007
	// The key is required before any port or gate call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return
	}
	idem.New(h.Idem, func(*http.Request) string { return actorOf(p) }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.proposeRole(w, r, p, userID) }),
	).ServeHTTP(w, r)
}

func (h *HTTP) proposeRole(w http.ResponseWriter, r *http.Request, p registry.Principal, userID string) {
	var b roleChangeBody
	if !idPattern.MatchString(userID) || decodeStrict(r, &b) != nil || !idPattern.MatchString(b.RoleID) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	// rule: ARC-I-006
	// A missing If-Match is refused before any port or gate call.
	if strings.TrimSpace(r.Header.Get("If-Match")) == "" {
		h.fail(w, r, http.StatusPreconditionRequired, server.CodePreconditionRequired, "If-Match is required")
		return
	}
	ctx := r.Context()
	u, err := h.Users.User(ctx, userID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if err := server.CheckIfMatch(r, int64(u.Version)); err != nil {
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	}
	if err := h.Users.CheckRoleChange(ctx, userID, b.RoleID); err != nil {
		h.writeErr(w, r, err)
		return
	}
	payload, err := json.Marshal(map[string]any{
		"role_id": strings.ToLower(b.RoleID),
		"change":  map[string]any{"user_id": strings.ToLower(userID), "from_role_id": u.RoleID, "user_version": u.Version},
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	var id gate.GateID
	err = h.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = h.Gates.Open(ctx, tx, GateRoleChange, gate.Subject{Kind: SubjectUser, ID: strings.ToLower(userID)}, payload, p)
		return err
	})
	if err != nil {
		h.writeErr(w, r, fmt.Errorf("open gate: %w", err))
		return
	}
	g, err := h.Gates.Get(ctx, id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, h.gateBody(g))
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
