package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// Gate kind slugs the gated payment routes open (spec/gates.json; G-B3 and G-B6). Their effects, not the
// routes, apply allocations and pay refunds.
const (
	KindAllocation = "billing.payment_allocation"
	KindRefund     = "billing.refund"
)

// Problem codes of the gated payment routes.
const (
	CodeAllocationSumMismatch = "allocation_sum_mismatch"
	CodeRefundExceedsPayment  = "refund_exceeds_payment"
)

// paymentVersion is a payment's row version as GET /payments/{payment_id} reports it.
const paymentVersion = 1

// ProposedAllocation is one row of a proposed allocation set, as the client asked for it.
// The bucket split is computed by the Proposals port (ARC-I-029), never taken from the body.
type ProposedAllocation struct {
	TargetType  string // bill, trust_ledger or unapplied
	TargetID    string // empty for unapplied
	AmountCents int64
}

// Proposals replaces a payment's proposed allocation set. It computes the bucket split and
// returns the proposed rows; it applies nothing (applying is the billing.payment_allocation
// effect's). A stale version returns server.ErrPreconditionFailed.
type Proposals interface {
	Propose(ctx context.Context, tx pgx.Tx, paymentID string, version int, rows []ProposedAllocation, by string) ([]Allocation, error)
}

// Gates is the part of the gate engine the gated routes use.
type Gates interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error)
	Get(ctx context.Context, id gate.GateID) (gate.Gate, error)
}

// TxRunner runs fn in one transaction; kernel/db's pool implements it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// Gated serves POST /payments/{payment_id}/allocations and POST /payments/{payment_id}/refunds.
// A trust-touching or ambiguous allocation set and every refund open a gate and answer 202;
// the routes never apply an allocation or pay a refund. Every dependency is required; a
// route answers 503 and calls nothing while one is missing.
type Gated struct {
	HTTP
	DB        TxRunner
	Gates     Gates
	Proposals Proposals
	// MinApprovers is each gate kind's approver count, for the Gate body; missing means 1.
	MinApprovers map[string]int
}

// Routes mounts the gated routes with their write declarations.
func (h *Gated) Routes(r chi.Router) {
	// rule: ARC-I-012
	// The route writes only the proposed set; applying it is billing.payment_allocation's.
	r.Method(http.MethodPost, "/payments/{payment_id}/allocations", server.Declare(h.allocate, "payment.allocation=proposed"))
	// rule: ARC-I-012
	// A refund is gate-owned: the route opens billing.refund and sets nothing itself.
	r.Method(http.MethodPost, "/payments/{payment_id}/refunds", server.Declare(h.refund))
}

func (h *Gated) gatedReady() bool {
	return h.ready() && h.DB != nil && h.Gates != nil && h.Proposals != nil
}

// begin runs the checks every gated route shares, in order: configured, authenticated,
// capability on the payment (404 when hidden), a well-formed id and an Idempotency-Key. It
// answers itself and returns false when the request cannot go on.
func (h *Gated) begin(w http.ResponseWriter, r *http.Request) (registry.Principal, string, bool) {
	if !h.gatedReady() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return registry.Principal{}, "", false
	}
	id := chi.URLParam(r, "payment_id")
	subject := registry.Subject{Kind: SubjectKind, ID: id}
	if !idPattern.MatchString(id) {
		subject = registry.Subject{}
	}
	p, ok := h.authorize(w, r, CapPayments, subject)
	if !ok {
		return registry.Principal{}, "", false
	}
	if subject.ID == "" {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "payment_id is not valid")
		return registry.Principal{}, "", false
	}
	// rule: ARC-I-007
	// The key is required before any service or gate call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return registry.Principal{}, "", false
	}
	return p, id, true
}

// once runs next under the idempotency store (ARC-I-008): a replay answers the first
// response without calling next again.
func (h *Gated) once(w http.ResponseWriter, r *http.Request, p registry.Principal, next http.HandlerFunc) {
	// rule: ARC-I-008
	idem.New(h.Idem, func(*http.Request) string { return p.UserID + p.RunID }, idem.DefaultTTL)(next).ServeHTTP(w, r)
}

// payment reads the payment; an unknown one answers 404.
func (h *Gated) payment(w http.ResponseWriter, r *http.Request, id string) (Payment, bool) {
	got, err := h.Payments.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such payment")
		return Payment{}, false
	}
	if err != nil {
		h.writeErr(w, r, err)
		return Payment{}, false
	}
	return got, true
}

type allocationsBody struct {
	Allocations []allocationBody `json:"allocations"`
}

func (b allocationsBody) valid() bool {
	if len(b.Allocations) == 0 {
		return false
	}
	for _, a := range b.Allocations {
		if !targetTypes[a.TargetType] || a.AmountCents < 0 || a.AmountCents > maxCents ||
			(a.TargetID != "" && !idPattern.MatchString(a.TargetID)) ||
			(a.TargetType == "unapplied") != (a.TargetID == "") {
			return false
		}
	}
	return true
}

// direct reports whether the set may be proposed without a gate: every row names one
// operating bill, each bill once. Trust-touching rows and unapplied money (client credit)
// always go through billing.payment_allocation.
func (b allocationsBody) direct() bool {
	seen := map[string]bool{}
	for _, a := range b.Allocations {
		if a.TargetType != "bill" || seen[a.TargetID] {
			return false
		}
		seen[a.TargetID] = true
	}
	return true
}

// allocate serves POST /payments/{payment_id}/allocations.
func (h *Gated) allocate(w http.ResponseWriter, r *http.Request) {
	p, id, ok := h.begin(w, r)
	if !ok {
		return
	}
	// rule: ARC-I-006
	if len(r.Header.Values("If-Match")) == 0 {
		h.fail(w, r, http.StatusPreconditionRequired, server.CodePreconditionRequired, "If-Match is required")
		return
	}
	h.once(w, r, p, func(w http.ResponseWriter, r *http.Request) { h.proposeAllocation(w, r, p, id) })
}

func (h *Gated) proposeAllocation(w http.ResponseWriter, r *http.Request, p registry.Principal, id string) {
	raw, err := io.ReadAll(r.Body)
	var b allocationsBody
	if err != nil || decodeStrict(raw, &b) != nil || !b.valid() {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	pay, ok := h.payment(w, r, id)
	if !ok {
		return
	}
	if server.CheckIfMatch(r, paymentVersion) != nil {
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	}
	var sum int64
	for _, a := range b.Allocations {
		sum += a.AmountCents
	}
	// rule: ARC-I-029
	// The set must account for the whole payment, no more and no less.
	if sum != pay.Capture.AmountCents {
		h.fail(w, r, http.StatusUnprocessableEntity, CodeAllocationSumMismatch, "Allocations must sum to the payment amount")
		return
	}
	if !b.direct() {
		payload, err := json.Marshal(map[string]any{"payment_id": id, "allocations": b.Allocations})
		if err != nil {
			h.writeErr(w, r, err)
			return
		}
		h.openGate(w, r, p, KindAllocation, id, payload)
		return
	}
	rows := make([]ProposedAllocation, 0, len(b.Allocations))
	for _, a := range b.Allocations {
		rows = append(rows, ProposedAllocation{TargetType: a.TargetType, TargetID: a.TargetID, AmountCents: a.AmountCents})
	}
	var as []Allocation
	err = h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var err error
		as, err = h.Proposals.Propose(ctx, tx, id, paymentVersion, rows, principalID(p))
		return err
	})
	switch {
	case errors.Is(err, server.ErrPreconditionFailed):
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrInvalid), errors.Is(err, ErrOtherClient):
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Allocation is not valid")
		return
	case err != nil:
		h.writeErr(w, r, err)
		return
	}
	v := viewOf(pay, as)
	for i := range v.Allocations {
		v.Allocations[i].State = "proposed"
	}
	if v.Status == "allocated" {
		v.Status = "allocation_pending"
	}
	server.SetETag(w, paymentVersion)
	writeJSON(w, http.StatusOK, v)
}

type refundBody struct {
	AmountCents *int64 `json:"amount_cents"`
	ReasonCode  string `json:"reason_code"`
}

// refund serves POST /payments/{payment_id}/refunds: it opens billing.refund. The refund is
// paid by the gate's effect after approval.
func (h *Gated) refund(w http.ResponseWriter, r *http.Request) {
	p, id, ok := h.begin(w, r)
	if !ok {
		return
	}
	h.once(w, r, p, func(w http.ResponseWriter, r *http.Request) { h.openRefund(w, r, p, id) })
}

func (h *Gated) openRefund(w http.ResponseWriter, r *http.Request, p registry.Principal, id string) {
	raw, err := io.ReadAll(r.Body)
	var b refundBody
	if err != nil || decodeStrict(raw, &b) != nil || b.AmountCents == nil || *b.AmountCents <= 0 ||
		*b.AmountCents > maxCents || strings.TrimSpace(b.ReasonCode) == "" || len(b.ReasonCode) > 64 {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	pay, ok := h.payment(w, r, id)
	if !ok {
		return
	}
	if *b.AmountCents > pay.Capture.AmountCents {
		h.fail(w, r, http.StatusConflict, CodeRefundExceedsPayment, "Refund exceeds the payment amount")
		return
	}
	// The refund goes back to the payer of record; the approver sees who in the payload.
	payload, err := json.Marshal(map[string]any{"payment_id": id, "amount_cents": *b.AmountCents,
		"payee": pay.Capture.ClientID, "reason": b.ReasonCode})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	h.openGate(w, r, p, KindRefund, id, payload)
}

func (h *Gated) openGate(w http.ResponseWriter, r *http.Request, p registry.Principal, kind, paymentID string, payload json.RawMessage) {
	var gid gate.GateID
	err := h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var err error
		gid, err = h.Gates.Open(ctx, tx, kind, gate.Subject{Kind: SubjectKind, ID: paymentID}, payload, p)
		return err
	})
	if err != nil {
		h.writeErr(w, r, fmt.Errorf("open gate: %w", err))
		return
	}
	g, err := h.Gates.Get(r.Context(), gid)
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

func (h *Gated) gateBody(g gate.Gate) gateBody {
	min := h.MinApprovers[g.Kind]
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
