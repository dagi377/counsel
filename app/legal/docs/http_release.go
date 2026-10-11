package docs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/api"
	kdocs "github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapRelease requests the release of a document outside the firm.
const CapRelease = "documents.release"

// OutgoingGate is the gate kind that approves sending a document out (spec gate G8). Only
// its effect marks a document released.
const OutgoingGate = "docs.outgoing"

// Gates is the part of the gate engine the release route uses.
type Gates interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error)
	Get(ctx context.Context, id gate.GateID) (gate.Gate, error)
}

// TxRunner runs fn in one transaction; kernel/db's pool implements it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// Draft is one document version an agent run drafted and no person has released yet.
type Draft struct {
	DocumentID string
	VersionID  kdocs.VersionID
	MatterID   string
	Kind       string
	RunID      string
	ActingFor  string
	CreatedAt  time.Time
}

// DraftFilter selects one page of drafts, ordered by document id. Zero fields do not filter.
type DraftFilter struct {
	After    string // return drafts with document id greater than this
	Limit    int
	MatterID string
}

// Drafts lists agent drafts. Boot implements it over docs.document and the agent run log.
type Drafts interface {
	List(ctx context.Context, f DraftFilter) ([]Draft, error)
}

// ReleaseHTTP serves POST /documents/{document_id}/release and GET /drafts. Release opens
// OutgoingGate and returns 202; it never releases a document itself. Every dependency is
// required; a route answers 503 and calls nothing while one is missing.
type ReleaseHTTP struct {
	Documents DocumentStore
	Drafts    Drafts
	Gates     Gates
	DB        TxRunner
	Access    Access
	Cursors   api.CursorCodec
	Idem      idem.Store
	Problems  server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

func init() { add(registerReleaseRoutes) }

// registerReleaseRoutes mounts the release and draft routes through Registry.Routes. Boot
// wiring supplies the dependencies; until it does, the routes answer 503.
func registerReleaseRoutes(r registry.Registry) error {
	r.Routes((&ReleaseHTTP{}).Routes)
	return nil
}

// Routes mounts the release and draft routes.
func (h *ReleaseHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// document.released is gate-owned (docs.outgoing): this route only opens the gate.
	r.Method(http.MethodPost, "/documents/{document_id}/release", server.Declare(h.release, "document.release_requested"))
	r.Get("/drafts", h.listDrafts)
}

func (h *ReleaseHTTP) ready() bool {
	return h.Documents != nil && h.Drafts != nil && h.Gates != nil && h.DB != nil && h.Access != nil &&
		h.Cursors != nil && h.Idem != nil && h.Problems != nil && h.Principal != nil
}

// base reuses the document routes' problem writing and authorization.
func (h *ReleaseHTTP) base() *HTTP {
	return &HTTP{Access: h.Access, Problems: h.Problems, Principal: h.Principal}
}

func (h *ReleaseHTTP) authorize(w http.ResponseWriter, r *http.Request, capability string, s registry.Subject) (registry.Principal, bool) {
	b := h.base()
	if !h.ready() {
		b.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return registry.Principal{}, false
	}
	return b.check(w, r, capability, s)
}

// releaseBody is the POST /documents/{document_id}/release body.
type releaseBody struct {
	VersionIDs []string `json:"version_ids"`
	Channel    string   `json:"channel"`
}

var releaseChannels = map[string]bool{"email": true, "portal": true, "esign": true, "filing": true}

func (b releaseBody) valid() bool {
	ok := releaseChannels[b.Channel] && len(b.VersionIDs) > 0 && len(b.VersionIDs) <= 50
	for _, v := range b.VersionIDs {
		ok = ok && idPattern.MatchString(v)
	}
	return ok
}

// release opens docs.outgoing for versions of one document. It never releases: only the
// gate's effect does, after a lawyer approves.
func (h *ReleaseHTTP) release(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "document_id")
	subject := registry.Subject{Kind: SubjectKind, ID: id}
	if !idPattern.MatchString(id) {
		subject = registry.Subject{}
	}
	p, ok := h.authorize(w, r, CapRelease, subject)
	if !ok {
		return
	}
	b := h.base()
	if subject.ID == "" {
		b.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "document_id is not valid")
		return
	}
	// rule: ARC-I-007
	// The key is required before any service or gate call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		b.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return
	}
	// rule: ARC-I-008
	idem.New(h.Idem, func(*http.Request) string { return p.UserID + p.RunID }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.openRelease(w, r, p, id) }),
	).ServeHTTP(w, r)
}

func (h *ReleaseHTTP) openRelease(w http.ResponseWriter, r *http.Request, p registry.Principal, id string) {
	b := h.base()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var in releaseBody
	if err != nil || decodeStrict(raw, &in) != nil || !in.valid() {
		b.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	d, err := h.Documents.Document(r.Context(), id)
	if err != nil {
		b.writeErr(w, r, err)
		return
	}
	for _, v := range in.VersionIDs {
		owned, err := h.Documents.HasVersion(r.Context(), id, kdocs.VersionID(v))
		if err != nil {
			b.writeErr(w, r, err)
			return
		}
		if !owned {
			b.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "A version is not of this document")
			return
		}
	}
	payload, err := json.Marshal(map[string]any{"matter_id": d.MatterID, "document_version_ids": in.VersionIDs,
		"channel": in.Channel})
	if err != nil {
		b.writeErr(w, r, err)
		return
	}
	// rule: ARC-I-012
	var gid gate.GateID
	err = h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var err error
		gid, err = h.Gates.Open(ctx, tx, OutgoingGate, gate.Subject{Kind: SubjectKind, ID: id}, payload, p)
		return err
	})
	if err != nil {
		b.writeErr(w, r, err)
		return
	}
	opened, err := h.Gates.Get(r.Context(), gid)
	if err != nil {
		b.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, gateBodyOf(opened, d.MatterID))
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

// gateBody is the openapi Gate.
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

// gateBodyOf renders an opened gate. docs.outgoing needs one approver (spec/gates.json).
func gateBodyOf(g gate.Gate, matterID string) gateBody {
	prep := actorBody{Type: string(g.Preparer.Kind), ID: g.Preparer.UserID}
	if g.Preparer.Kind == registry.PrincipalAgent {
		prep = actorBody{Type: "agent", ID: g.Preparer.RunID, ActingFor: g.Preparer.ActingFor}
	}
	subject := map[string]any{"type": g.Subject.Kind, "id": g.Subject.ID}
	if matterID != "" {
		subject["matter_id"] = matterID
	}
	return gateBody{
		ID: string(g.ID), Kind: g.Kind, Version: g.Version, State: gateStates[g.Status],
		Subject: subject, Payload: g.Payload, PayloadHash: g.PayloadHash,
		Preparers: []actorBody{prep}, MinApprovers: 1,
		OpenedAt:  g.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: g.ExpiresAt.UTC().Format(time.RFC3339),
		Decisions: []any{},
	}
}

// draftView is one agent draft as the API returns it.
type draftView struct {
	DocumentID string `json:"document_id"`
	VersionID  string `json:"version_id"`
	MatterID   string `json:"matter_id,omitempty"`
	Kind       string `json:"kind"`
	RunID      string `json:"run_id,omitempty"`
	ActingFor  string `json:"acting_for,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// draftPage is the listDrafts body.
type draftPage struct {
	Data       []draftView `json:"data"`
	NextCursor *string     `json:"next_cursor"`
}

// listDrafts serves GET /drafts: agent drafts waiting for a person, one page at a time.
func (h *ReleaseHTTP) listDrafts(w http.ResponseWriter, r *http.Request) {
	matter := r.URL.Query().Get("matter_id")
	subject := registry.Subject{}
	if idPattern.MatchString(matter) {
		subject = registry.Subject{Kind: matterkind.Subject, ID: matter}
	}
	if _, ok := h.authorize(w, r, CapView, subject); !ok {
		return
	}
	b := h.base()
	if matter != "" && subject.ID == "" {
		b.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "matter_id is not valid")
		return
	}
	cur, err := api.FromRequest(r, h.Cursors)
	if errors.Is(err, api.ErrBadLimit) {
		b.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "limit must be an integer")
		return
	}
	if err != nil {
		b.writeErr(w, r, err)
		return
	}
	got, err := h.Drafts.List(r.Context(), DraftFilter{After: cur.After, Limit: cur.Limit + 1, MatterID: matter})
	if err != nil {
		b.writeErr(w, r, err)
		return
	}
	page := draftPage{Data: []draftView{}}
	for i, d := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: got[i-1].DocumentID, Limit: cur.Limit})
			if err != nil {
				b.writeErr(w, r, err)
				return
			}
			page.NextCursor = &next
			break
		}
		page.Data = append(page.Data, draftView{DocumentID: d.DocumentID, VersionID: string(d.VersionID),
			MatterID: d.MatterID, Kind: d.Kind, RunID: d.RunID, ActingFor: d.ActingFor,
			CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, page)
}
