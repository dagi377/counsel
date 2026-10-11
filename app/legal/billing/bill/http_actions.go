package bill

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

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Problem codes the bill action routes write.
const (
	CodeBillDeliveredUseCreditNote = "bill_delivered_use_credit_note"
	CodeBillNotApproved            = "bill_not_approved"
	CodeBillNotSubmittable         = "bill_not_submittable"
)

// SubjectBill is the gate and outbox subject kind of a bill.
const SubjectBill = "billing.bill"

// MessageDeliveryRequested is the outbox message type that queues a bill delivery. The
// delivery effect sets delivered_at on evidence; the route never does.
const MessageDeliveryRequested = "billing.bill.delivery_requested"

// Gates is the part of the gate engine the action routes use.
type Gates interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error)
	Get(ctx context.Context, id gate.GateID) (gate.Gate, error)
}

// Emitter is the part of kernel/outbox the deliver route uses.
type Emitter interface {
	Emit(ctx context.Context, tx pgx.Tx, s registry.Subject, typ string, body any) error
}

// ActionHTTP serves POST /bills/{bill_id}/submit, /void and /deliver. Submit and void open
// their gates and return 202; deliver queues one outbox message and returns 202. None of
// them approves, numbers, voids or delivers a bill itself. Every dependency is required; a
// route answers 503 and calls nothing while one is missing.
type ActionHTTP struct {
	Bills    Service
	Gates    Gates
	Outbox   Emitter
	DB       TxRunner
	Access   Access
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
	// MinApprovers is each gate kind's approver count, for the Gate body; missing means 1.
	MinApprovers map[string]int
}

// Routes mounts the action routes with their write declarations.
func (h *ActionHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// Approval, void and delivered_at are gate- or effect-owned: these routes only open the
	// gate or queue the delivery, and declare no gate-owned key.
	r.Method(http.MethodPost, "/bills/{bill_id}/submit", server.Declare(h.action(CapDraft, parseSubmit, h.submit), "bill.approval_requested"))
	r.Method(http.MethodPost, "/bills/{bill_id}/void", server.Declare(h.action(CapApprove, parseVoid, h.void), "bill.void_requested"))
	r.Method(http.MethodPost, "/bills/{bill_id}/deliver", server.Declare(h.action(CapDraft, parseDeliver, h.deliver), "bill.delivery_requested"))
}

func (h *ActionHTTP) gw() gateway {
	return gateway{problems: h.Problems, access: h.Access, principal: h.Principal}
}

func (h *ActionHTTP) ready() bool {
	return h.Bills != nil && h.Gates != nil && h.Outbox != nil && h.DB != nil && h.Access != nil &&
		h.Idem != nil && h.Problems != nil && h.Principal != nil
}

// bodyParser validates an action's request body before any service call; ok false is 400.
type bodyParser func(body []byte) (in any, ok bool)

// step runs one action on a loaded bill at the If-Match version with the parsed body.
type step func(w http.ResponseWriter, r *http.Request, p registry.Principal, b Bill, in any)

// action wraps a step with the checks every action shares, in order: 503, 401, 403 (cap),
// 400 malformed id, 428 missing If-Match, 400 missing Idempotency-Key, then idempotency
// replay, 400 invalid body, 404 (missing or walled), 412 stale version.
func (h *ActionHTTP) action(capability string, parse bodyParser, fn step) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g := h.gw()
		p, ok := g.authorize(w, r, h.ready(), capability)
		if !ok {
			return
		}
		id, ok := g.billID(w, r)
		if !ok {
			return
		}
		// rule: ARC-I-006
		if len(r.Header.Values("If-Match")) == 0 {
			g.fail(w, r, http.StatusPreconditionRequired, server.CodePreconditionRequired, "If-Match is required")
			return
		}
		// rule: ARC-I-007
		// The key is required before any service or gate call.
		if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
			g.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
			return
		}
		// rule: ARC-I-008
		idem.New(h.Idem, func(*http.Request) string { return p.UserID }, idem.DefaultTTL)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				in, ok := parse(body)
				if err != nil || !ok {
					g.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
					return
				}
				ctx := r.Context()
				b, err := h.Bills.Get(ctx, id)
				if err == nil {
					err = g.visible(ctx, p, capability, b)
				}
				if err != nil {
					g.readErr(w, r, err)
					return
				}
				if err := server.CheckIfMatch(r, int64(b.Version)); err != nil {
					g.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
					return
				}
				fn(w, r, p, b, in)
			}),
		).ServeHTTP(w, r)
	}
}

// decodeBody decodes an optional JSON body strictly; an empty body leaves v unchanged.
func decodeBody(body []byte, v any) error {
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

var deliveryMethods = map[string]bool{"email": true, "portal": true, "mail": true}

type submitBody struct {
	DeliveryMethod string `json:"delivery_method,omitempty"`
}

// submit opens billing.bill_approval for a draft. It never approves: only the gate's effect
// does, after a decision by someone other than the preparer.
func (h *ActionHTTP) submit(w http.ResponseWriter, r *http.Request, p registry.Principal, b Bill, parsed any) {
	g := h.gw()
	in := parsed.(submitBody)
	if b.Status != StatusDraft {
		g.fail(w, r, http.StatusConflict, CodeBillNotSubmittable, "Only a draft bill can be submitted")
		return
	}
	// rule: BIL-C-120
	payload, err := json.Marshal(map[string]any{"bill_id": string(b.ID), "matter_id": b.MatterID,
		"total_cents": b.TotalCents, "delivery_method": in.DeliveryMethod})
	if err != nil {
		g.readErr(w, r, err)
		return
	}
	h.openGate(w, r, p, GateBillApproval, b, payload)
}

// parseSubmit reads the optional submit body; delivery_method defaults to email.
func parseSubmit(body []byte) (any, bool) {
	in := submitBody{DeliveryMethod: "email"}
	return in, decodeBody(body, &in) == nil && deliveryMethods[in.DeliveryMethod]
}

type voidBody struct {
	ReasonCode string `json:"reason_code"`
}

// parseVoid reads the void body; reason_code is required.
func parseVoid(body []byte) (any, bool) {
	var in voidBody
	err := decodeBody(body, &in)
	return in, err == nil && strings.TrimSpace(in.ReasonCode) != "" && len(in.ReasonCode) <= 64
}

// void opens billing.bill_void for an approved, undelivered bill. A delivered bill is
// corrected with a credit note.
func (h *ActionHTTP) void(w http.ResponseWriter, r *http.Request, p registry.Principal, b Bill, parsed any) {
	g := h.gw()
	in := parsed.(voidBody)
	switch {
	case !b.DeliveredAt.IsZero() || b.Status == StatusDelivered || b.Status == StatusPartiallyPaid ||
		b.Status == StatusPaid || b.Status == StatusWrittenOff || b.Status == StatusCredited:
		g.fail(w, r, http.StatusConflict, CodeBillDeliveredUseCreditNote, "Bill is delivered; use a credit note")
		return
	case b.Status != StatusApproved:
		g.fail(w, r, http.StatusConflict, CodeBillNotApproved, "Only an approved bill can be voided")
		return
	}
	payload, err := json.Marshal(map[string]any{"bill_id": string(b.ID), "reason": in.ReasonCode})
	if err != nil {
		g.readErr(w, r, err)
		return
	}
	h.openGate(w, r, p, GateBillVoid, b, payload)
}

func (h *ActionHTTP) openGate(w http.ResponseWriter, r *http.Request, p registry.Principal, kind string, b Bill, payload json.RawMessage) {
	g := h.gw()
	subject := gate.Subject{Kind: SubjectBill, ID: string(b.ID)}
	var id gate.GateID
	err := h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = h.Gates.Open(ctx, tx, kind, subject, payload, p)
		return err
	})
	if err != nil {
		g.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	}
	opened, err := h.Gates.Get(r.Context(), id)
	if err != nil {
		g.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, h.gateBody(opened, b.MatterID))
}

type deliverBody struct {
	Channel             string   `json:"channel"`
	RecipientContactIDs []string `json:"recipient_contact_ids,omitempty"`
}

var deliverChannels = map[string]bool{"portal": true, "email": true, "print": true}

// parseDeliver reads the deliver body; channel is required.
func parseDeliver(body []byte) (any, bool) {
	var in deliverBody
	ok := decodeBody(body, &in) == nil && deliverChannels[in.Channel]
	for _, c := range in.RecipientContactIDs {
		ok = ok && uuidPattern.MatchString(c)
	}
	return in, ok
}

// deliver queues delivery of an approved bill. delivered_at stays unset: the delivery
// effect sets it on evidence.
func (h *ActionHTTP) deliver(w http.ResponseWriter, r *http.Request, p registry.Principal, b Bill, parsed any) {
	g := h.gw()
	in := parsed.(deliverBody)
	if b.Status != StatusApproved || !b.DeliveredAt.IsZero() {
		g.fail(w, r, http.StatusConflict, CodeBillNotApproved, "Only an approved, undelivered bill can be delivered")
		return
	}
	msg := map[string]any{"bill_id": string(b.ID), "channel": in.Channel, "recipient_contact_ids": in.RecipientContactIDs}
	err := h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		return h.Outbox.Emit(ctx, tx, registry.Subject{Kind: SubjectBill, ID: string(b.ID)}, MessageDeliveryRequested, msg)
	})
	if err != nil {
		g.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, viewOf(b))
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

func (h *ActionHTTP) gateBody(g gate.Gate, matterID string) gateBody {
	min := h.MinApprovers[g.Kind]
	if min == 0 {
		min = 1
	}
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
		Preparers: []actorBody{prep}, MinApprovers: min,
		OpenedAt:  g.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: g.ExpiresAt.UTC().Format(time.RFC3339),
		Decisions: []any{},
	}
}
