package credit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Capabilities the credit note routes check (spec/legal/access.json).
const (
	// CapView reads credit notes and write-offs.
	CapView = "billing.view"
	// CapCredit requests a credit note or write-off.
	CapCredit = "billing.credit"
)

// GateCreditNote is the gate kind POST /credit-notes opens. Its effect, not the route,
// assigns the number and issues the note.
const GateCreditNote = "billing.credit_note"

// Subject kinds the routes check access on.
const (
	BillSubject = "billing.bill"
	NoteSubject = "billing.credit_note"
)

// Problem codes the credit note routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
	CodeBillNotDelivered  = "bill_not_delivered"
)

// Entry kinds and statuses as the API names them.
const (
	EntryCredit    = "credit"
	EntryWriteOff  = "write_off"
	EntryRequested = "requested"
	EntryIssued    = "issued"
	EntryRejected  = "rejected"
)

// Entry is a credit note or write-off as the read routes return it. A requested entry has
// no Number: only the gate's effect assigns one.
type Entry struct {
	ID          string
	Number      string
	BillID      string
	Kind        string
	AmountCents int64
	HSTCents    int64
	ReasonCode  string
	Status      string
	GateID      string
}

// ListFilter selects one page of entries: those whose id sorts after After, on BillID when
// set, at most Limit.
type ListFilter struct {
	BillID string
	After  string
	Limit  int
}

// Reads lists and reads credit notes and write-offs, with their gate state, under the
// caller's RLS session. Service has no list; this port serves GET /credit-notes (HTTP-09).
type Reads interface {
	Entries(ctx context.Context, f ListFilter) ([]Entry, error)
	// Entry returns ErrNotFound for an unknown or hidden id.
	Entry(ctx context.Context, id string) (Entry, error)
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

// HTTP serves GET /credit-notes, POST /credit-notes and GET /credit-notes/{credit_note_id}.
// Every dependency is required; a route answers 503 and calls nothing while one is missing.
type HTTP struct {
	Reads    Reads
	Bills    Bills
	Access   Access
	DB       TxRunner
	Gates    Gates
	Idem     idem.Store
	Cursors  api.CursorCodec
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
	// MinApprovers is the billing.credit_note approver count for the Gate body; 0 means 1.
	MinApprovers int
}

// idPattern is the openapi Id format (uuid).
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Routes mounts the credit note routes.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/credit-notes", h.list)
	// rule: ARC-I-012
	// Issuing and numbering are gate-owned: the route opens billing.credit_note and sets
	// nothing itself.
	r.Method(http.MethodPost, "/credit-notes", server.Declare(h.request))
	r.Get("/credit-notes/{credit_note_id}", h.get)
}

// noteView is the openapi CreditNote.
type noteView struct {
	ID          string  `json:"id"`
	Number      *string `json:"number"`
	BillID      string  `json:"bill_id"`
	Kind        string  `json:"kind"`
	AmountCents int64   `json:"amount_cents"`
	HSTCents    int64   `json:"hst_cents"`
	ReasonCode  string  `json:"reason_code,omitempty"`
	Status      string  `json:"status"`
	GateID      string  `json:"gate_id,omitempty"`
}

func viewOf(e Entry) noteView {
	v := noteView{ID: e.ID, BillID: e.BillID, Kind: e.Kind, AmountCents: e.AmountCents, HSTCents: e.HSTCents,
		ReasonCode: e.ReasonCode, Status: e.Status, GateID: e.GateID}
	if e.Number != "" {
		n := e.Number
		v.Number = &n
	}
	return v
}

func (h *HTTP) ready() bool {
	return h.Reads != nil && h.Bills != nil && h.Access != nil && h.DB != nil && h.Gates != nil &&
		h.Idem != nil && h.Cursors != nil && h.Problems != nil && h.Principal != nil
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

// writeErr maps an unexpected error to a problem. The error text never reaches the body.
func (h *HTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, api.ErrBadCursor) {
		h.problems().WriteProblem(w, r, api.BadCursorProblem())
		return
	}
	h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
}

// authorize answers 503, 401, 403 or 404 itself and returns false when the request cannot go
// on. It runs before any service or gate call.
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
		h.writeErr(w, r, err)
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

// list serves GET /credit-notes: entries by id, one page at a time, optionally for one bill.
func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
	billID := r.URL.Query().Get("bill_id")
	subject := registry.Subject{}
	if billID != "" && idPattern.MatchString(billID) {
		subject = registry.Subject{Kind: BillSubject, ID: billID}
	}
	if _, ok := h.authorize(w, r, CapView, subject); !ok {
		return
	}
	if billID != "" && !idPattern.MatchString(billID) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "bill_id is not valid")
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
	got, err := h.Reads.Entries(r.Context(), ListFilter{BillID: billID, After: cur.After, Limit: cur.Limit + 1})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := api.Page[noteView]{Items: []noteView{}}
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

// get serves GET /credit-notes/{credit_note_id}. A note hidden by a wall or RLS answers 404.
func (h *HTTP) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "credit_note_id")
	if !idPattern.MatchString(id) {
		if _, ok := h.authorize(w, r, CapView, registry.Subject{}); !ok {
			return
		}
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "credit_note_id is not valid")
		return
	}
	if _, ok := h.authorize(w, r, CapView, registry.Subject{Kind: NoteSubject, ID: id}); !ok {
		return
	}
	e, err := h.Reads.Entry(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such credit note")
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOf(e))
}

// requestBody is the openapi CreditNoteCreate.
type requestBody struct {
	BillID      string `json:"bill_id"`
	Kind        string `json:"kind"`
	AmountCents *int64 `json:"amount_cents"`
	ReasonCode  string `json:"reason_code"`
	TrustEffect string `json:"trust_effect,omitempty"`
}

// maxCents is the openapi Cents maximum.
const maxCents = 100_000_000_000

func (b requestBody) valid() bool {
	return idPattern.MatchString(b.BillID) && (b.Kind == EntryCredit || b.Kind == EntryWriteOff) &&
		b.AmountCents != nil && *b.AmountCents > 0 && *b.AmountCents <= maxCents &&
		b.ReasonCode != "" && len(b.ReasonCode) <= 64 &&
		(b.TrustEffect == "" || b.TrustEffect == "none" || b.TrustEffect == "refund_to_trust")
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

// request serves POST /credit-notes: it opens gate billing.credit_note and answers 202 with
// the Gate. The note gets its number only when the gate's effect runs.
func (h *HTTP) request(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r, CapCredit, registry.Subject{})
	if !ok {
		return
	}
	// rule: ARC-I-007
	// The key is required before any service or gate call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.problems().WriteProblem(w, r, server.NewProblem(http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required"))
		return
	}
	idem.New(h.Idem, func(*http.Request) string { return principalID(p) }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.openNote(w, r, p) }),
	).ServeHTTP(w, r)
}

func (h *HTTP) openNote(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	var b requestBody
	if err := decodeStrict(r, &b); err != nil || !b.valid() {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	if b.TrustEffect == "" {
		b.TrustEffect = "none"
	}
	ctx := r.Context()
	allowed, err := h.Access.Can(ctx, p, CapCredit, registry.Subject{Kind: BillSubject, ID: b.BillID})
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such bill")
		return
	case err != nil:
		h.writeErr(w, r, err)
		return
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return
	}
	bill, err := h.Bills.Bill(ctx, b.BillID)
	switch {
	case errors.Is(err, ErrNotFound):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such bill")
		return
	case err != nil:
		h.writeErr(w, r, err)
		return
	case !bill.Delivered:
		h.fail(w, r, http.StatusConflict, CodeBillNotDelivered, "Bill is not delivered; void or edit it instead")
		return
	case *b.AmountCents > bill.OpenCents:
		// rule: BIL-C-082
		h.problems().WriteProblem(w, r, *exceedsBill())
		return
	}
	payload, err := json.Marshal(b)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	var id gate.GateID
	err = h.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = h.Gates.Open(ctx, tx, GateCreditNote, gate.Subject{Kind: BillSubject, ID: b.BillID}, payload, p)
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
