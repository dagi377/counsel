package withdraw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapRequest is the capability to request or cancel a trust withdrawal.
const CapRequest = "trust.request"

// GateFeeKind is the gate a withdrawal that pays a bill opens instead of GateKind.
const GateFeeKind = "billing.trust_fee_withdrawal"

// SubjectWithdrawal is the gate subject kind of a withdrawal.
const SubjectWithdrawal = "trust.withdrawal"

// Problem codes the request routes write besides the shared ones.
const (
	CodeAlreadyExecuted = "already_executed"
)

// StatusCancelled is a withdrawal cancelled before execution. Only RequestHTTP's cancel
// route records it, through Cancels.
const StatusCancelled Status = "cancelled"

// LedgerReader resolves a client ledger to its account and matter.
type LedgerReader interface {
	// Ledger returns one ledger, or trust.ErrNotFound.
	Ledger(ctx context.Context, id trust.LedgerID) (trust.Ledger, error)
}

// Gates is the part of gate.Engine the request routes use.
type Gates interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error)
	Withdraw(ctx context.Context, tx pgx.Tx, id gate.GateID, by gate.Principal) error
}

// Row is what the API shows of a withdrawal that Withdrawal does not carry.
type Row struct {
	Version  int64
	LedgerID string
	BillID   string
	// GateID is the gate the request opened (trust.withdrawal_request.gate_id); empty for a
	// withdrawal requested before it was stored.
	GateID string
}

// Cancels reads a withdrawal's row and records its cancellation. Service has no Cancel
// (its contract is frozen), so the cancel route uses this port.
type Cancels interface {
	// Row returns the withdrawal's row version, ledger, bill and gate, or ErrNotFound.
	Row(ctx context.Context, id string) (Row, error)
	// RecordGate stores the gate the request opened on the withdrawal, in tx.
	RecordGate(ctx context.Context, tx pgx.Tx, id, gateID string) error
	// Cancel marks the withdrawal cancelled in tx and returns its new version.
	Cancel(ctx context.Context, tx pgx.Tx, id string, by registry.Principal) (int64, error)
}

// RequestHTTP serves POST /trust/withdrawals and POST /trust/withdrawals/{withdrawal_id}/cancel.
// A request records the withdrawal and opens its gate; it never authorizes. Every dependency
// is required; a route answers 503 and calls nothing while one is missing.
type RequestHTTP struct {
	Withdrawals Service
	Ledgers     LedgerReader
	Gates       Gates
	Cancels     Cancels
	DB          TxRunner
	Access      Access
	Idem        idem.Store
	Problems    server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// Routes mounts the request routes with their write declarations.
func (h *RequestHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// withdrawal.status=authorized is gate-owned: these routes record requested or cancelled
	// and open or withdraw the gate, never authorize.
	r.Method(http.MethodPost, "/trust/withdrawals", server.Declare(h.guarded(false, h.request), "withdrawal.status=requested"))
	r.Method(http.MethodPost, "/trust/withdrawals/{withdrawal_id}/cancel", server.Declare(h.guarded(true, h.cancel), "withdrawal.status=cancelled"))
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (h *RequestHTTP) ready() bool {
	return h.Withdrawals != nil && h.Ledgers != nil && h.Gates != nil && h.Cancels != nil && h.DB != nil &&
		h.Access != nil && h.Idem != nil && h.Problems != nil && h.Principal != nil
}

func (h *RequestHTTP) pw() server.ProblemWriter {
	if h.Problems == nil {
		return &server.Problems{}
	}
	return h.Problems
}

func (h *RequestHTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	h.pw().WriteProblem(w, r, server.NewProblem(status, code, title))
}

type handler func(w http.ResponseWriter, r *http.Request, p registry.Principal, body []byte)

// guarded runs the checks both routes share, in order: 503, 401, 403, 400 malformed id,
// 428 missing If-Match (cancel), 400 missing Idempotency-Key, then idempotency replay.
func (h *RequestHTTP) guarded(ifMatch bool, fn handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.ready() {
			h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
			return
		}
		p, ok := h.Principal(r)
		if !ok {
			h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
			return
		}
		// rule: ARC-I-051
		// Fail closed: an error or a missing capability stops the request before any call.
		allowed, err := h.Access.Can(r.Context(), p, CapRequest, registry.Subject{})
		switch {
		case errors.Is(err, identity.ErrNotVisible):
			h.fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
			return
		case err != nil:
			h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
			return
		case !allowed:
			h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
			return
		}
		// rule: ARC-I-027
		// Every request and cancel has a person behind it: the user, or the user an agent run
		// acts for. An agent with no acting user is refused before any call.
		if PreparerOf(p) == "" {
			h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Request has no acting user")
			return
		}
		if id := chi.URLParam(r, "withdrawal_id"); ifMatch && !uuidPattern.MatchString(id) {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "withdrawal_id is not valid")
			return
		}
		// rule: ARC-I-006
		if ifMatch && len(r.Header.Values("If-Match")) == 0 {
			h.fail(w, r, http.StatusPreconditionRequired, server.CodePreconditionRequired, "If-Match is required")
			return
		}
		// rule: ARC-I-007
		// The key is required before any service or gate call.
		if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
			h.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
			return
		}
		// rule: ARC-I-008
		idem.New(h.Idem, func(*http.Request) string { return idemScope(p) }, idem.DefaultTTL)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
					return
				}
				fn(w, r, p, body)
			}),
		).ServeHTTP(w, r)
	}
}

// visible checks matterID for p; a matter hidden by a wall or RLS is not found.
func (h *RequestHTTP) visible(ctx context.Context, p registry.Principal, matterID string) error {
	// rule: ARC-I-042
	allowed, err := h.Access.Can(ctx, p, CapRequest, registry.Subject{Kind: matterkind.Subject, ID: matterID})
	switch {
	case err != nil:
		return err
	case !allowed:
		return identity.ErrNotVisible
	}
	return nil
}

type createBody struct {
	LedgerID               string `json:"ledger_id"`
	AmountCents            int64  `json:"amount_cents"`
	PurposeCategory        string `json:"purpose_category"`
	Payee                  string `json:"payee"`
	Method                 string `json:"method"`
	BillID                 string `json:"bill_id,omitempty"`
	DirectionDocumentID    string `json:"direction_document_id,omitempty"`
	ReceivingAccountMasked string `json:"receiving_account_masked,omitempty"`
}

var purposes = map[string]bool{"fees": true, "disbursement": true, "client_payment": true, "third_party": true, "refund": true}

// parseCreate decodes the WithdrawalCreate body strictly; ok false is 400.
func parseCreate(body []byte) (createBody, bool) {
	var in createBody
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || dec.More() {
		return in, false
	}
	ok := uuidPattern.MatchString(in.LedgerID) && in.AmountCents > 0 && purposes[in.PurposeCategory] &&
		strings.TrimSpace(in.Payee) != "" && len(in.Payee) <= 200 &&
		(in.Method == "eft" || in.Method == "cheque" || in.Method == "wire") &&
		(in.BillID == "" || uuidPattern.MatchString(in.BillID)) &&
		(in.DirectionDocumentID == "" || uuidPattern.MatchString(in.DirectionDocumentID))
	return in, ok
}

// request records the withdrawal as requested and opens its gate. It never authorizes:
// Authorize is the gate effect, reached only after a decision by someone else.
func (h *RequestHTTP) request(w http.ResponseWriter, r *http.Request, p registry.Principal, body []byte) {
	in, ok := parseCreate(body)
	if !ok {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	if in.Method == "wire" {
		// The withdrawal module has no wire method; a wire is requested as an EFT.
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Method wire is not supported")
		return
	}
	ctx := r.Context()
	l, err := h.Ledgers.Ledger(ctx, trust.LedgerID(in.LedgerID))
	if err == nil {
		err = h.visible(ctx, p, l.MatterID)
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	wd, err := h.Withdrawals.Request(ctx, Request{
		TrustAccountID: string(l.AccountID), MatterID: l.MatterID, AmountCents: in.AmountCents,
		Payee: in.Payee, Method: Method(in.Method), EFTAuthorizationID: in.DirectionDocumentID,
		Reason: in.PurposeCategory, PreparedBy: PreparerOf(p), IdempotencyKey: r.Header.Get(idem.HeaderName),
		BillID: in.BillID,
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	// rule: BIL-L-035 (verify)
	// A withdrawal that pays a bill is a fee transfer and opens the fee gate.
	kind := GateKind
	if in.BillID != "" {
		kind = GateFeeKind
	}
	payload, err := json.Marshal(map[string]any{"withdrawal_id": wd.ID, "ledger_id": in.LedgerID,
		"matter_id": l.MatterID, "amount_cents": in.AmountCents, "payee": in.Payee, "method": in.Method,
		"purpose_category": in.PurposeCategory, "bill_id": in.BillID})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	var gid gate.GateID
	err = h.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		gid, err = h.Gates.Open(ctx, tx, kind, gate.Subject{Kind: SubjectWithdrawal, ID: wd.ID}, payload, p)
		if err != nil {
			return err
		}
		// The gate id is stored in the tx that opens it, so cancel can withdraw it.
		return h.Cancels.RecordGate(ctx, tx, wd.ID, string(gid))
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	wd.GateID = string(gid)
	writeJSON(w, http.StatusAccepted, viewOf(wd, in.LedgerID, in.BillID, 1, p))
}

// cancel withdraws the open gate and records the withdrawal cancelled. An executed
// withdrawal cannot be cancelled.
func (h *RequestHTTP) cancel(w http.ResponseWriter, r *http.Request, p registry.Principal, body []byte) {
	if strings.TrimSpace(string(body)) != "" {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Cancel takes no body")
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "withdrawal_id")
	wd, err := h.Withdrawals.Get(ctx, id)
	if err == nil {
		err = h.visible(ctx, p, wd.Request.MatterID)
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	row, err := h.Cancels.Row(ctx, id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	version := row.Version
	// rule: ARC-I-027
	// Only the preparer cancels: for an agent, the user it acts for.
	if by := PreparerOf(p); by == "" || by != wd.Request.PreparedBy {
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Only the preparer can cancel the request")
		return
	}
	gateID := row.GateID
	if gateID == "" {
		gateID = wd.GateID
	}
	if err := server.CheckIfMatch(r, version); err != nil {
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	}
	switch wd.Status {
	case StatusExecuted:
		h.fail(w, r, http.StatusConflict, CodeAlreadyExecuted, "Withdrawal is already executed")
		return
	case StatusRejected, StatusCancelled:
		h.fail(w, r, http.StatusConflict, server.CodeVersionConflict, "Withdrawal is already closed")
		return
	}
	err = h.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if gateID != "" {
			if err := h.Gates.Withdraw(ctx, tx, gate.GateID(gateID), p); err != nil {
				return err
			}
		}
		var err error
		version, err = h.Cancels.Cancel(ctx, tx, id, p)
		return err
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	wd.Status = StatusCancelled
	writeJSON(w, http.StatusOK, viewOf(wd, row.LedgerID, row.BillID, version, registry.Principal{Kind: registry.PrincipalUser, UserID: wd.Request.PreparedBy}))
}

// idemScope keys idempotency replays by actor: the user, or the agent run and its user.
func idemScope(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return "agent:" + p.RunID + ":" + p.ActingFor
	}
	return p.UserID
}

// PreparerOf is the user who prepares what p requests: the user, or the user an agent acts
// for. It is empty for an agent with no acting user and for a guest.
func PreparerOf(p registry.Principal) string {
	switch p.Kind {
	case registry.PrincipalAgent:
		return p.ActingFor
	case registry.PrincipalUser:
		return p.UserID
	}
	return ""
}

// writeErr maps a service error to its problem. Error text never reaches the body.
func (h *RequestHTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, trust.ErrNotFound), errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such withdrawal or ledger")
	case errors.Is(err, ErrUnverified):
		h.fail(w, r, http.StatusConflict, CodeUnverified, "Trust withdrawal rules are not verified")
	case errors.Is(err, ErrTrustOverdraw):
		p := server.NewProblem(http.StatusConflict, CodeTrustOverdraw, "Amount exceeds the available trust balance")
		p.Rule = "ARC-I-025"
		h.pw().WriteProblem(w, r, p)
	case errors.Is(err, ErrUnclearedFunds):
		h.fail(w, r, http.StatusConflict, CodeUnclearedFunds, "Withdrawal draws on uncleared funds")
	case errors.Is(err, ErrNoEFTAuthorization):
		h.fail(w, r, http.StatusConflict, CodeNoEFTAuthorization, "EFT has no signed authorization record")
	case errors.Is(err, gate.ErrNotPreparer):
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Only the preparer can cancel the request")
	case errors.Is(err, ErrBearerPayee), errors.Is(err, ErrInvalid):
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Withdrawal is not valid")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

func viewOf(wd Withdrawal, ledgerID, billID string, version int64, by registry.Principal) withdrawalView {
	actor := actorView{Type: "user", ID: by.UserID}
	if by.Kind == registry.PrincipalAgent {
		actor = actorView{Type: "agent", ID: by.RunID, ActingFor: by.ActingFor}
	}
	v := withdrawalView{ID: wd.ID, Version: version, LedgerID: ledgerID, AmountCents: wd.Request.AmountCents,
		PurposeCategory: wd.Request.Reason, Payee: wd.Request.Payee, Method: string(wd.Request.Method),
		BillID: billID, Status: apiStatus[wd.Status], GateID: wd.GateID, InitiatedBy: actor,
		ExecutedBy: wd.ExecutedBy}
	if wd.AuthorizedBy != "" {
		v.AuthorizedBy = []string{wd.AuthorizedBy}
	}
	return v
}
