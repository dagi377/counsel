package credit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// WriteOffCloser closes a submitted write-off draft without approving it; PGWriteOffs
// implements it. Every method runs in the caller's tx.
type WriteOffCloser interface {
	// Lock reads write-off id FOR UPDATE, or returns ErrNotFound.
	Lock(ctx context.Context, tx pgx.Tx, id string) (WriteOff, error)
	// CreditGate returns the G-B5 gate the draft was submitted under, or "" when none.
	CreditGate(ctx context.Context, tx pgx.Tx, id string) (string, error)
	// Close moves the submitted draft to StatusRejected or StatusWithdrawn.
	Close(ctx context.Context, tx pgx.Tx, id string, to Status) error
}

var _ WriteOffCloser = PGWriteOffs{}

const writeOffCreditGateSQL = `SELECT coalesce(credit_gate_id::text, '') FROM billing.write_off_draft WHERE id = $1::uuid`

// CreditGate implements WriteOffCloser.
func (PGWriteOffs) CreditGate(ctx context.Context, tx pgx.Tx, id string) (string, error) {
	var gateID string
	err := tx.QueryRow(ctx, writeOffCreditGateSQL, id).Scan(&gateID)
	if notFound(err) {
		return "", fmt.Errorf("write-off %s credit gate: %w", id, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("write-off %s credit gate: %w", id, err)
	}
	return gateID, nil
}

// RejectHooks returns the billing.credit_note rejection hook: a rejected write-off gate
// closes its draft as StatusRejected. A credit-note payload closes nothing here.
func RejectHooks(c WriteOffCloser) map[string]registry.RejectFunc {
	return map[string]registry.RejectFunc{GateKindCreditNote: (&WriteOffReject{Store: c}).OnReject}
}

// WriteOffReject closes a write-off draft when its G-B5 gate is rejected (WO-FIX).
type WriteOffReject struct {
	Store WriteOffCloser
}

var _ registry.RejectHook = (*WriteOffReject)(nil)

// OnReject implements registry.RejectHook for billing.credit_note.
func (h *WriteOffReject) OnReject(ctx context.Context, tx pgx.Tx, g registry.Rejected) error {
	var p creditNotePayload
	if err := json.Unmarshal(g.Payload, &p); err != nil {
		return fmt.Errorf("write-off reject %s: payload: %w", g.GateID, ErrInvalidState)
	}
	if p.Kind != payloadKindWriteOff {
		return nil
	}
	if h.Store == nil || g.Subject.ID == "" {
		return fmt.Errorf("write-off reject %s: %w", g.GateID, ErrInvalidState)
	}
	// rule: BIL-C-085
	// A rejected draft is final and no longer counts toward the second-approval total.
	if err := h.Store.Close(ctx, tx, g.Subject.ID, StatusRejected); err != nil {
		return fmt.Errorf("write-off reject %s: %w", g.GateID, err)
	}
	return nil
}

// GateWithdrawer withdraws a pending gate on its preparer's request; gate.Engine implements it.
type GateWithdrawer interface {
	Withdraw(ctx context.Context, tx pgx.Tx, id gate.GateID, by gate.Principal) error
}

// CodeNotPreparer is the problem code when someone other than the preparer withdraws.
const CodeNotPreparer = "not_preparer"

// WithdrawHTTP serves POST /credit-notes/{credit_note_id}/withdraw for a write-off draft:
// its preparer takes it back, which withdraws the pending G-B5 gate and closes the draft as
// StatusWithdrawn. Only the preparer may withdraw. Every dependency is required; the route
// answers 503 and calls nothing while one is missing.
type WithdrawHTTP struct {
	Store     WriteOffCloser
	Gates     GateWithdrawer
	DB        TxRunner
	Access    Access
	Idem      idem.Store
	Problems  server.ProblemWriter
	Principal func(r *http.Request) (registry.Principal, bool)
}

// Routes mounts the withdraw route.
func (h *WithdrawHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// The route withdraws the gate and closes the draft; it never approves.
	r.Method(http.MethodPost, "/credit-notes/{credit_note_id}/withdraw", server.Declare(h.withdraw, "write_off.status=withdrawn"))
}

func (h *WithdrawHTTP) ready() bool {
	return h.Store != nil && h.Gates != nil && h.DB != nil && h.Access != nil && h.Idem != nil &&
		h.Problems != nil && h.Principal != nil
}

func (h *WithdrawHTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	pw := h.Problems
	if pw == nil {
		pw = &server.Problems{}
	}
	pw.WriteProblem(w, r, server.NewProblem(status, code, title))
}

func (h *WithdrawHTTP) withdraw(w http.ResponseWriter, r *http.Request) {
	if !h.ready() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return
	}
	p, ok := h.Principal(r)
	if !ok {
		h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "credit_note_id")
	if !idPattern.MatchString(id) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "credit_note_id is not valid")
		return
	}
	allowed, err := h.Access.Can(r.Context(), p, CapCredit, registry.Subject{Kind: SubjectWriteOff, ID: id})
	switch {
	case err != nil:
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such write-off")
		return
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return
	}
	// rule: ARC-I-007
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return
	}
	idem.New(h.Idem, func(*http.Request) string { return principalID(p) }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.close(w, r, p, id) }),
	).ServeHTTP(w, r)
}

// close withdraws the draft's gate and closes the draft, in one tx.
func (h *WithdrawHTTP) close(w http.ResponseWriter, r *http.Request, p registry.Principal, id string) {
	err := h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		return WithdrawWriteOff(ctx, tx, h.Store, h.Gates, id, p)
	})
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": string(StatusWithdrawn)})
	case errors.Is(err, ErrNotFound):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such write-off")
	case errors.Is(err, gate.ErrNotPreparer):
		h.fail(w, r, http.StatusForbidden, CodeNotPreparer, "Only the preparer may withdraw")
	case errors.Is(err, ErrInvalidState), errors.Is(err, gate.ErrNotPending):
		h.fail(w, r, http.StatusConflict, "invalid_state", "The write-off is not an open draft")
	default:
		h.fail(w, r, http.StatusInternalServerError, "internal", "Internal error")
	}
}

// WithdrawWriteOff is the preparer taking back submitted write-off draft id in tx: it
// withdraws the pending G-B5 gate and closes the draft as StatusWithdrawn. Anyone other
// than the preparer, an agent included, gets gate.ErrNotPreparer.
func WithdrawWriteOff(ctx context.Context, tx pgx.Tx, s WriteOffCloser, g GateWithdrawer, id string, by registry.Principal) error {
	wo, err := s.Lock(ctx, tx, id)
	if err != nil {
		return fmt.Errorf("withdraw write-off: %w", err)
	}
	// rule: ARC-I-016
	// Only the preparer, as a person, takes a draft back.
	if by.Kind != registry.PrincipalUser || by.UserID == "" || by.UserID != wo.PreparedBy {
		return fmt.Errorf("withdraw write-off %s: %w", id, gate.ErrNotPreparer)
	}
	if wo.Status != StatusDraft {
		return fmt.Errorf("withdraw write-off %s: status %s: %w", id, wo.Status, ErrInvalidState)
	}
	gateID, err := s.CreditGate(ctx, tx, id)
	if err != nil {
		return fmt.Errorf("withdraw write-off: %w", err)
	}
	if gateID == "" {
		return fmt.Errorf("withdraw write-off %s: not submitted: %w", id, ErrInvalidState)
	}
	if err := g.Withdraw(ctx, tx, gate.GateID(gateID), by); err != nil {
		return fmt.Errorf("withdraw write-off %s: %w", id, err)
	}
	if err := s.Close(ctx, tx, id, StatusWithdrawn); err != nil {
		return fmt.Errorf("withdraw write-off: %w", err)
	}
	return nil
}
