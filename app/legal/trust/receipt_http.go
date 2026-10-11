package trust

import (
	"context"
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

// CapRecord posts trust receipts (spec/legal/access.json). It is also the approver capability
// of trust.receipt_classification; the engine keeps the recorder from approving.
const CapRecord = "trust.record"

// SetReceiptPending is the state key POST /trust/receipts sets. It is route-owned: posting to
// the client ledger (trust_txn.posted=receipt) stays with the classification gate.
const SetReceiptPending = "trust_receipt.status=received_pending_classification"

// CodeNoLedger is the problem code when the matter has no ledger on the account.
const CodeNoLedger = "no_client_ledger"

// GateOpener is the part of the gate engine the receipt route uses.
type GateOpener interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error)
	Get(ctx context.Context, id gate.GateID) (gate.Gate, error)
}

// TxRunner runs fn in one transaction; kernel/db's pool implements it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// ReceiptHTTP serves POST /trust/receipts (Q-HTTP-16). The route records the receipt in the
// trust receipts journal the day it arrives and, in the same transaction, opens
// trust.receipt_classification with the proposed classification. It never posts to a client
// ledger: only the gate effect does. Every dependency is required; the route answers 503
// and calls nothing while one is missing.
type ReceiptHTTP struct {
	Lists    Lists
	Journal  ReceiptJournal
	Access   Access
	DB       TxRunner
	Gates    GateOpener
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
	// MinApprovers is the trust.receipt_classification approver count for the Gate body; 0
	// means 1.
	MinApprovers int
}

// Routes mounts POST /trust/receipts.
func (h *ReceiptHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// The route records the receipt and opens the gate; posting is gate-owned.
	r.Method(http.MethodPost, "/trust/receipts", server.Declare(h.record, SetReceiptPending))
}

func init() { add(registerReceiptRoute) }

// registerReceiptRoute mounts POST /trust/receipts. Boot wiring supplies the dependencies;
// until it does, the route answers 503.
func registerReceiptRoute(r registry.Registry) error {
	r.Routes((&ReceiptHTTP{}).Routes)
	return nil
}

func (h *ReceiptHTTP) ready() bool {
	return h.Lists != nil && h.Journal != nil && h.Access != nil && h.DB != nil && h.Gates != nil &&
		h.Idem != nil && h.Problems != nil && h.Principal != nil
}

func (h *ReceiptHTTP) problems() server.ProblemWriter {
	if h.Problems == nil {
		return &server.Problems{}
	}
	return h.Problems
}

func (h *ReceiptHTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	h.problems().WriteProblem(w, r, server.NewProblem(status, code, title))
}

// can answers 404, 403 or 500 itself and returns false when p may not record on s.
func (h *ReceiptHTTP) can(w http.ResponseWriter, r *http.Request, p registry.Principal, s registry.Subject) bool {
	// rule: ARC-I-051
	allowed, err := h.Access.Can(r.Context(), p, CapRecord, s)
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
		return false
	case err != nil:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return false
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return false
	}
	return true
}

func (h *ReceiptHTTP) record(w http.ResponseWriter, r *http.Request) {
	if !h.ready() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return
	}
	p, ok := h.Principal(r)
	if !ok {
		h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return
	}
	if !h.can(w, r, p, registry.Subject{}) {
		return
	}
	// rule: ARC-I-007
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.problems().WriteProblem(w, r, server.NewProblem(http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required"))
		return
	}
	idem.New(h.Idem, func(*http.Request) string { return preparerOf(p) }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.open(w, r, p) }),
	).ServeHTTP(w, r)
}

// receiptBody is the openapi TrustReceiptCreate.
type receiptBody struct {
	AccountID    string `json:"account_id"`
	MatterID     string `json:"matter_id"`
	ReceivedOn   string `json:"received_on"`
	AmountCents  *int64 `json:"amount_cents"`
	Method       string `json:"method"`
	Payer        string `json:"payer"`
	InstrumentNo string `json:"instrument_no,omitempty"`
	Purpose      string `json:"purpose,omitempty"`
	PaymentID    string `json:"payment_id,omitempty"`
}

// receiptMethods maps the openapi methods to trust methods. An Interac e-Transfer is an EFT.
var receiptMethods = map[string]Method{
	"eft": MethodEFT, "cheque": MethodCheque, "cash": MethodCash, "wire": MethodWire,
	"card": MethodCard, "interac": MethodEFT,
}

// maxReceiptCents is the openapi Cents maximum.
const maxReceiptCents = 100_000_000_000

// defaultReceiptPurpose fills the optional openapi purpose; the journal needs one.
const defaultReceiptPurpose = "trust receipt"

func (b receiptBody) parse() (JournalReceipt, bool) {
	m, ok := receiptMethods[b.Method]
	day, err := time.ParseInLocation(time.DateOnly, b.ReceivedOn, trustZone)
	payer := strings.TrimSpace(b.Payer)
	if !ok || err != nil || !idPattern.MatchString(b.AccountID) || !idPattern.MatchString(b.MatterID) ||
		b.AmountCents == nil || *b.AmountCents <= 0 || *b.AmountCents > maxReceiptCents ||
		payer == "" || len(b.Payer) > 200 || len(b.InstrumentNo) > 40 || len(b.Purpose) > 200 ||
		(b.PaymentID != "" && !idPattern.MatchString(b.PaymentID)) {
		return JournalReceipt{}, false
	}
	purpose := strings.TrimSpace(b.Purpose)
	if purpose == "" {
		purpose = defaultReceiptPurpose
	}
	return JournalReceipt{
		AccountID: AccountID(b.AccountID), MatterID: b.MatterID, ReceivedOn: day, AmountCents: *b.AmountCents,
		Method: m, Payer: payer, InstrumentNo: b.InstrumentNo, Purpose: purpose, PaymentID: b.PaymentID,
		Proposed: ClassTrust, Status: ReceiptPending,
	}, true
}

func (h *ReceiptHTTP) open(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	var b receiptBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil || dec.More() {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	rec, ok := b.parse()
	if !ok {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	if !h.can(w, r, p, registry.Subject{Kind: matterkind.Subject, ID: rec.MatterID}) {
		return
	}
	ctx := r.Context()
	ledgers, err := h.Lists.ListLedgers(ctx, LedgerFilter{MatterID: rec.MatterID, AccountID: string(rec.AccountID), Limit: 1})
	if err != nil {
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	}
	if len(ledgers) == 0 {
		h.fail(w, r, http.StatusUnprocessableEntity, CodeNoLedger, "The matter has no client ledger on this trust account")
		return
	}
	rec.LedgerID = ledgers[0].ID
	rec.RecordedBy = preparerOf(p)
	var id gate.GateID
	// rule: ARC-I-012
	// One transaction: the journal row (same-day record) and the classification gate commit
	// together, so a pending receipt always has its gate.
	err = h.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rid, err := h.Journal.Record(ctx, tx, rec)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(ClassificationPayload{
			ReceiptID: rid, MatterID: rec.MatterID, AmountCents: rec.AmountCents, Classification: rec.Proposed,
		})
		if err != nil {
			return err
		}
		if id, err = h.Gates.Open(ctx, tx, GateReceiptClassification, gate.Subject{Kind: SubjectReceipt, ID: rid}, payload, p); err != nil {
			return fmt.Errorf("open gate: %w", err)
		}
		return h.Journal.SetGate(ctx, tx, rid, string(id))
	})
	if err != nil {
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	}
	g, err := h.Gates.Get(ctx, id)
	if err != nil {
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, h.gateBody(g))
}

// receiptGateStates maps engine states to the openapi GateState.
var receiptGateStates = map[gate.Status]string{
	gate.StatusPending:          "open",
	gate.StatusApprovedCooling:  "approved_cooling",
	gate.StatusApproved:         "approved",
	gate.StatusRejected:         "rejected",
	gate.StatusChangesRequested: "returned",
	gate.StatusWithdrawn:        "withdrawn",
	gate.StatusExpired:          "expired",
	gate.StatusSuperseded:       "withdrawn",
}

type receiptActor struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	ActingFor string `json:"acting_for,omitempty"`
}

// receiptGate is the openapi Gate.
type receiptGate struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Version      int             `json:"version"`
	State        string          `json:"state"`
	Subject      map[string]any  `json:"subject"`
	Payload      json.RawMessage `json:"payload"`
	PayloadHash  string          `json:"payload_hash"`
	Preparers    []receiptActor  `json:"preparers"`
	MinApprovers int             `json:"min_approvers"`
	OpenedAt     string          `json:"opened_at"`
	ExpiresAt    string          `json:"expires_at"`
	Decisions    []any           `json:"decisions"`
}

func (h *ReceiptHTTP) gateBody(g gate.Gate) receiptGate {
	minApprovers := max(h.MinApprovers, 1)
	prep := receiptActor{Type: string(g.Preparer.Kind), ID: g.Preparer.UserID}
	if g.Preparer.Kind == registry.PrincipalAgent {
		prep = receiptActor{Type: "agent", ID: g.Preparer.RunID, ActingFor: g.Preparer.ActingFor}
	}
	return receiptGate{
		ID: string(g.ID), Kind: g.Kind, Version: g.Version, State: receiptGateStates[g.Status],
		Subject: map[string]any{"type": g.Subject.Kind, "id": g.Subject.ID}, Payload: g.Payload,
		PayloadHash: g.PayloadHash, Preparers: []receiptActor{prep}, MinApprovers: minApprovers,
		OpenedAt:  g.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: g.ExpiresAt.UTC().Format(time.RFC3339),
		Decisions: []any{},
	}
}
