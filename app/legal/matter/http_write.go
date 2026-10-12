package matter

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Gate kinds the matter write routes open. Their effects, not the routes, write the matter.
const (
	// EventPartyAdded (a2): parties were added to a matter's conflict check. The payload holds
	// ids only, never a party's name.
	EventPartyAdded = "matter.party_added"
	GateOpening     = "matter.opening"
	GateWidenAccess = "matter.widen_access"
	GateClosing     = "platform.closing"
)

// Problem codes of the matter write routes.
const (
	CodeConflictNotCleared = "conflict_not_cleared"
	CodeInvalidTransition  = "invalid_transition"
)

// ContactSubject is the subject kind of a party contact: an opening gate is about the client
// until the effect creates the matter.
const ContactSubject = "party.contact"

// PlainChange is an edit of fields no gate owns. Nil means unchanged.
//
// CourtProvince and PropertyProvince (additive, Q-TAX-API) are the matter's
// place-of-supply facts: a province or territory code (ProvinceCodes), or "" to clear it.
type PlainChange struct {
	ShortName        *string
	PracticeAreaID   *string
	CourtProvince    *string
	PropertyProvince *string
}

// ProvinceCodes are the Canadian province and territory codes a matter's court or property
// province may hold.
var ProvinceCodes = map[string]bool{
	"AB": true, "BC": true, "MB": true, "NB": true, "NL": true, "NS": true, "NT": true,
	"NU": true, "ON": true, "PE": true, "QC": true, "SK": true, "YT": true,
}

// validProvince reports whether v is unset, a clear ("") or a province or territory code.
//
// rule: BIL-T-011
// rule: BIL-T-012
func validProvince(v *string) bool { return v == nil || *v == "" || ProvinceCodes[*v] }

// Updater writes plain matter fields at an expected row version. A stale version returns
// server.ErrPreconditionFailed.
type Updater interface {
	Update(ctx context.Context, id ID, version int, c PlainChange, by registry.Principal) (Matter, error)
}

// Transitioner checks a status change without writing it (Lifecycle).
type Transitioner interface {
	Transition(ctx context.Context, m Matter, actorID string, to Status) (Matter, error)
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

// Writes serves POST /matters and PATCH /matters/{matter_id}. Changes a gate owns (opening,
// team, restriction, status) open the gate and return 202; the route never writes them.
type Writes struct {
	HTTP
	DB        TxRunner
	Gates     Gates
	Updates   Updater
	Lifecycle Transitioner
	Idem      idem.Store
	// MinApprovers is each gate kind's approver count, for the Gate body; missing means 1.
	MinApprovers map[string]int
}

// WriteRoutes mounts the write routes with their write declarations.
func (h *Writes) WriteRoutes(r chi.Router) {
	// rule: ARC-I-012
	// Opening is gate-owned: the route opens matter.opening and sets nothing itself.
	r.Method(http.MethodPost, "/matters", server.Declare(h.create))
	r.Method(http.MethodPatch, "/matters/{matter_id}", server.Declare(h.update, "matter.short_name", "matter.practice_area_id",
		"matter.court_province", "matter.property_province"))
}

func (h *Writes) writeReady() bool {
	return h.ready() && h.DB != nil && h.Gates != nil && h.Updates != nil && h.Lifecycle != nil && h.Idem != nil
}

// writePrincipal is principal for the write routes.
func (h *Writes) writePrincipal(w http.ResponseWriter, r *http.Request) (registry.Principal, bool) {
	if !h.writeReady() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return registry.Principal{}, false
	}
	return h.principal(w, r)
}

type openingBody struct {
	Number              string `json:"number"`
	ShortName           string `json:"short_name"`
	ClientID            string `json:"client_id"`
	PracticeAreaID      string `json:"practice_area_id"`
	ResponsibleLawyerID string `json:"responsible_lawyer_id"`
	Visibility          string `json:"visibility,omitempty"`
}

// openingGatePayload is the matter.opening payload POST /matters puts on the gate.
type openingGatePayload struct {
	openingBody
	MatterID     string `json:"matter_id"`
	PracticeArea string `json:"practice_area"`
}

// newMatterID returns a random (version 4) uuid for a matter the opening gate will create.
func newMatterID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("matter id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
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

func validVisibility(v string) bool {
	switch Visibility(v) {
	case "", FirmOpen, TeamOnly, Restricted:
		return true
	}
	return false
}

// create serves POST /matters: it opens gate matter.opening. The matter row is written by
// the gate's effect after approval.
func (h *Writes) create(w http.ResponseWriter, r *http.Request) {
	p, ok := h.writePrincipal(w, r)
	if !ok {
		return
	}
	// rule: ARC-I-051
	allowed, err := h.Access.Can(r.Context(), p, CapOpen, registry.Subject{})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if !allowed {
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return
	}
	// rule: ARC-I-007
	// The key is required before any service or gate call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.problems().WriteProblem(w, r, server.NewProblem(http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required"))
		return
	}
	// rule: ARC-I-008
	idem.New(h.Idem, func(*http.Request) string { return principalID(p) }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.openMatter(w, r, p) }),
	).ServeHTTP(w, r)
}

func (h *Writes) openMatter(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	var b openingBody
	if err := decodeStrict(r, &b); err != nil || b.Number == "" || b.ShortName == "" || !idPattern.MatchString(b.ClientID) ||
		b.PracticeAreaID == "" || b.ResponsibleLawyerID == "" || !validVisibility(b.Visibility) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	// The route picks the new matter's id, so the one approval of matter.opening both
	// creates and opens that matter (OpeningEffect); practice_area is the gate schema's name.
	id, err := newMatterID()
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	payload, err := json.Marshal(openingGatePayload{openingBody: b, MatterID: id, PracticeArea: b.PracticeAreaID})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	subject := gate.Subject{Kind: ContactSubject, ID: b.ClientID}
	h.openGate(w, r, p, GateOpening, subject, payload)
}

func (h *Writes) openGate(w http.ResponseWriter, r *http.Request, p registry.Principal, kind string, s gate.Subject, payload json.RawMessage) {
	var id gate.GateID
	err := h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = h.Gates.Open(ctx, tx, kind, s, payload, p)
		return err
	})
	if err != nil {
		h.writeErr(w, r, fmt.Errorf("open gate: %w", err))
		return
	}
	g, err := h.Gates.Get(r.Context(), id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, h.gateBody(g))
}

type patchBody struct {
	ShortName        *string          `json:"short_name"`
	PracticeAreaID   *string          `json:"practice_area_id"`
	CourtProvince    *string          `json:"court_province"`
	PropertyProvince *string          `json:"property_province"`
	Status           *string          `json:"status"`
	Restricted       *bool            `json:"restricted"`
	Team             *json.RawMessage `json:"team"`
}

func (b patchBody) gated() bool { return b.Status != nil || b.Restricted != nil || b.Team != nil }
func (b patchBody) plain() bool {
	return b.ShortName != nil || b.PracticeAreaID != nil || b.CourtProvince != nil || b.PropertyProvince != nil
}

// update serves PATCH /matters/{matter_id}. Plain fields are written at the If-Match
// version (200 and the new ETag); team, restriction and status open a gate (202) and leave
// the matter unchanged.
func (h *Writes) update(w http.ResponseWriter, r *http.Request) {
	p, ok := h.writePrincipal(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "matter_id")
	if !idPattern.MatchString(id) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "matter_id is not valid")
		return
	}
	subject := registry.Subject{Kind: matterkind.Subject, ID: id}
	// rule: ARC-I-051
	allowed, err := h.Access.Can(ctx, p, CapEdit, subject)
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such matter")
		return
	case err != nil:
		h.writeErr(w, r, err)
		return
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return
	}
	// rule: ARC-I-006
	if len(r.Header.Values("If-Match")) == 0 {
		h.fail(w, r, http.StatusPreconditionRequired, server.CodePreconditionRequired, "If-Match is required")
		return
	}
	var b patchBody
	if err := decodeStrict(r, &b); err != nil || (!b.gated() && !b.plain()) || (b.gated() && b.plain()) ||
		(b.ShortName != nil && *b.ShortName == "") || (b.PracticeAreaID != nil && *b.PracticeAreaID == "") ||
		!validProvince(b.CourtProvince) || !validProvince(b.PropertyProvince) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	m, err := h.Query.Get(ctx, ID(id))
	if errors.Is(err, ErrNotFound) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such matter")
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if err := server.CheckIfMatch(r, int64(m.RowVersion)); err != nil {
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	}
	if b.gated() {
		h.gatedUpdate(w, r, p, m, b)
		return
	}
	out, err := h.Updates.Update(ctx, m.ID, m.RowVersion, PlainChange{ShortName: b.ShortName, PracticeAreaID: b.PracticeAreaID,
		CourtProvince: b.CourtProvince, PropertyProvince: b.PropertyProvince}, p)
	switch {
	case errors.Is(err, server.ErrPreconditionFailed):
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	case errors.Is(err, ErrNotFound):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such matter")
		return
	case errors.Is(err, ErrInvalid):
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Change is not valid")
		return
	case err != nil:
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(out.RowVersion))
	writeJSON(w, http.StatusOK, viewOf(out, true))
}

// gatedUpdate opens the gate that owns the requested change. Status, restriction and team
// changes in one request each need their own request: one gate per change.
func (h *Writes) gatedUpdate(w http.ResponseWriter, r *http.Request, p registry.Principal, m Matter, b patchBody) {
	n := 0
	for _, set := range []bool{b.Status != nil, b.Restricted != nil, b.Team != nil} {
		if set {
			n++
		}
	}
	if n != 1 {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Change status, restriction or team in separate requests")
		return
	}
	kind := GateWidenAccess
	change := map[string]any{"matter_id": string(m.ID), "version": m.RowVersion}
	switch {
	case b.Status != nil:
		to := Status(*b.Status)
		// Dry run: Lifecycle checks walls, the transition and the conflict clearance; the
		// gate's effect applies it.
		if _, err := h.Lifecycle.Transition(r.Context(), m, principalID(p), to); err != nil {
			h.transitionProblem(w, r, err)
			return
		}
		kind = GateClosing
		if to == Open {
			kind = GateOpening
			// The matter.opening schema also names the responsible lawyer and practice area.
			change["responsible_lawyer_id"] = m.ResponsibleLawyerID
			change["practice_area"] = m.PracticeAreaID
		}
		change["status"] = string(to)
	case b.Restricted != nil:
		change["restricted"] = *b.Restricted
		change["visibility"] = accessVisibility(b.Restricted, m.Visibility)
	default:
		var team any
		if err := json.Unmarshal(*b.Team, &team); err != nil {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "team is not valid")
			return
		}
		if _, ok := team.([]any); !ok {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "team must be a list")
			return
		}
		change["team"] = team
		change["visibility"] = accessVisibility(nil, m.Visibility)
	}
	payload, err := json.Marshal(change)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	h.openGate(w, r, p, kind, gate.Subject{Kind: matterkind.Subject, ID: string(m.ID)}, payload)
}

// accessVisibility is the required matter.widen_access visibility for a PATCH route change,
// derived server-side: the audience the matter has once the change applies. Restricting
// keeps it to the team, opening gives it to the firm, and a team change keeps the matter's
// current audience. The effect applies restricted and team, not this field (WidenAccessPayload).
func accessVisibility(restricted *bool, current Visibility) string {
	switch {
	case restricted != nil && *restricted:
		return "team"
	case restricted != nil, current == FirmOpen, current == "":
		return "firm"
	}
	return "team"
}

func (h *Writes) transitionProblem(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such matter")
	case errors.Is(err, ErrNotCleared):
		h.fail(w, r, http.StatusConflict, CodeConflictNotCleared, "Conflict check is not cleared")
	case errors.Is(err, ErrBadTransition):
		h.fail(w, r, http.StatusConflict, CodeInvalidTransition, "Status change is not allowed")
	default:
		h.writeErr(w, r, err)
	}
}

func principalID(p registry.Principal) string {
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

func (h *Writes) gateBody(g gate.Gate) gateBody {
	min := h.MinApprovers[g.Kind]
	if min == 0 {
		min = 1
	}
	prep := actorBody{Type: string(g.Preparer.Kind), ID: g.Preparer.UserID}
	if g.Preparer.Kind == registry.PrincipalAgent {
		prep = actorBody{Type: "agent", ID: g.Preparer.RunID, ActingFor: g.Preparer.ActingFor}
	}
	subject := map[string]any{"type": g.Subject.Kind, "id": g.Subject.ID}
	if g.Subject.Kind == matterkind.Subject {
		subject["matter_id"] = g.Subject.ID
	}
	return gateBody{
		ID: string(g.ID), Kind: g.Kind, Version: g.Version, State: gateStates[g.Status],
		Subject: subject, Payload: g.Payload, PayloadHash: g.PayloadHash,
		Preparers: []actorBody{prep}, MinApprovers: min,
		OpenedAt:  g.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: g.ExpiresAt.UTC().Format(time.RFC3339),
		Decisions: []any{},
	}
}
