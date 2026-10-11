package recon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapReconcile is the capability to import bank lines and prepare or submit a reconciliation.
const CapReconcile = "trust.reconcile"

// GateSignOff is the gate a submitted reconciliation opens; its effect signs off.
const GateSignOff = "trust.reconciliation_signoff"

// Subject kinds the write routes check and open gates on.
const (
	SubjectAccount        = "trust.account"
	SubjectReconciliation = "trust.reconciliation"
)

// Problem codes the write routes write besides the shared ones.
const (
	CodeDuplicateStatement    = "duplicate_statement"
	CodePeriodLocked          = "period_locked"
	CodeUnexplainedDifference = "unexplained_difference"
	CodeUnparseableStatement  = "unparseable_statement"
	CodeNoStatementParser     = "no_statement_parser"
	CodeBankBalanceMismatch   = "bank_balance_mismatch"
	CodeUnverified            = "trust_rules_unverified"
)

// ErrUnparseable is returned by a Parser for a statement file it cannot read (422).
var ErrUnparseable = errors.New("recon: statement file is not parseable")

// maxStatementBytes bounds an uploaded statement file.
const maxStatementBytes = 10 << 20

// Parser turns an uploaded statement file (CSV or OFX) into a Statement for the account.
type Parser interface {
	// Parse returns the statement, or ErrUnparseable.
	Parse(ctx context.Context, accountID string, file []byte) (Statement, error)
}

// Versions reads a reconciliation's row version. Reconciliation carries none and Service
// is frozen, so the submit route uses this port.
type Versions interface {
	Version(ctx context.Context, id string) (int64, error)
}

// Statements reads an imported statement's closing balance. Service is frozen and has no
// statement read, so the prepare route uses this port.
type Statements interface {
	// Closing returns the closing balance of the account's imported statement for p, or
	// ErrNotFound when none is imported.
	Closing(ctx context.Context, accountID string, p Period) (int64, error)
}

// Gates is the part of gate.Engine the submit route uses.
type Gates interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error)
	Get(ctx context.Context, id gate.GateID) (gate.Gate, error)
}

// TxRunner runs fn in one transaction; kernel/db's pool implements it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// WriteHTTP serves POST /trust/bank-lines/imports, POST /trust/reconciliations and POST
// /trust/reconciliations/{reconciliation_id}/submit. Import records bank lines and posts no
// journal entry; submit opens the sign-off gate and never approves. Every dependency is
// required; a route answers 503 and calls nothing while one is missing.
type WriteHTTP struct {
	Recons     Service
	Parser     Parser
	Statements Statements
	Versions   Versions
	Gates      Gates
	DB         TxRunner
	Access     Access
	Idem       idem.Store
	Problems   server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
	// MinApprovers is the sign-off gate's approver count for the Gate body; 0 means 1.
	MinApprovers int
}

// Routes mounts the write routes with their write declarations.
func (h *WriteHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// reconciliation.status=signed_off is gate-owned: submit only opens the gate.
	r.Method(http.MethodPost, "/trust/bank-lines/imports", server.Declare(h.guarded(false, h.importLines), "bank_line.imported"))
	r.Method(http.MethodPost, "/trust/reconciliations", server.Declare(h.guarded(false, h.prepare), "reconciliation.status=prepared"))
	r.Method(http.MethodPost, "/trust/reconciliations/{reconciliation_id}/submit", server.Declare(h.guarded(true, h.submit), "reconciliation.signoff_requested"))
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var monthPattern = regexp.MustCompile(`^([0-9]{4})-(0[1-9]|1[0-2])$`)

func (h *WriteHTTP) ready() bool {
	return h.Recons != nil && h.Parser != nil && h.Statements != nil && h.Versions != nil && h.Gates != nil && h.DB != nil &&
		h.Access != nil && h.Idem != nil && h.Problems != nil && h.Principal != nil
}

func (h *WriteHTTP) pw() server.ProblemWriter {
	if h.Problems == nil {
		return &server.Problems{}
	}
	return h.Problems
}

func (h *WriteHTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	h.pw().WriteProblem(w, r, server.NewProblem(status, code, title))
}

type handler func(w http.ResponseWriter, r *http.Request, p registry.Principal)

// guarded runs the checks every write route shares, in order: 503, 401, 403, 400 malformed
// id, 428 missing If-Match (submit), 400 missing Idempotency-Key, then idempotency replay.
func (h *WriteHTTP) guarded(ifMatch bool, fn handler) http.HandlerFunc {
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
		allowed, err := h.Access.Can(r.Context(), p, CapReconcile, registry.Subject{})
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
		if ifMatch && !uuidPattern.MatchString(chi.URLParam(r, "reconciliation_id")) {
			h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "reconciliation_id is not valid")
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
		idem.New(h.Idem, func(*http.Request) string { return p.UserID }, idem.DefaultTTL)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fn(w, r, p) }),
		).ServeHTTP(w, r)
	}
}

// visible checks the trust account for p; an account hidden by RLS is not found.
func (h *WriteHTTP) visible(ctx context.Context, p registry.Principal, accountID string) error {
	// rule: ARC-I-042
	allowed, err := h.Access.Can(ctx, p, CapReconcile, registry.Subject{Kind: SubjectAccount, ID: accountID})
	switch {
	case err != nil:
		return err
	case !allowed:
		return identity.ErrNotVisible
	}
	return nil
}

// importLines reads the multipart upload (account_id, file), parses it and imports the
// statement's lines. It posts no journal entry: differences become reconciling work.
func (h *WriteHTTP) importLines(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	accountID, file, ok := readUpload(r)
	if !ok {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Upload is not valid")
		return
	}
	ctx := r.Context()
	if err := h.visible(ctx, p, accountID); err != nil {
		h.writeErr(w, r, err)
		return
	}
	s, err := h.Parser.Parse(ctx, accountID, file)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	s.AccountID = accountID
	// rule: ARC-U-032
	id, err := h.Recons.ImportStatement(ctx, s)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"statement_id": id, "lines": len(s.Lines)})
}

// readUpload reads exactly the fields account_id and file; anything else is invalid.
func readUpload(r *http.Request) (string, []byte, bool) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxStatementBytes+1<<16)
	if err := r.ParseMultipartForm(maxStatementBytes); err != nil {
		return "", nil, false
	}
	form := r.MultipartForm
	if len(form.Value) != 1 || len(form.Value["account_id"]) != 1 || len(form.File) != 1 || len(form.File["file"]) != 1 {
		return "", nil, false
	}
	accountID := form.Value["account_id"][0]
	if !uuidPattern.MatchString(accountID) {
		return "", nil, false
	}
	f, err := form.File["file"][0].Open()
	if err != nil {
		return "", nil, false
	}
	defer f.Close()
	file, err := io.ReadAll(io.LimitReader(f, maxStatementBytes+1))
	if err != nil || len(file) == 0 || len(file) > maxStatementBytes {
		return "", nil, false
	}
	return accountID, file, true
}

type prepareBody struct {
	AccountID        string `json:"account_id"`
	Month            string `json:"month"`
	BankBalanceCents *int64 `json:"bank_balance_cents"`
}

// prepare computes the month's three-way reconciliation from the imported statement.
func (h *WriteHTTP) prepare(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	var in prepareBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || dec.More() || !uuidPattern.MatchString(in.AccountID) || in.BankBalanceCents == nil {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	period, ok := parseMonth(in.Month)
	if !ok {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "month must be YYYY-MM")
		return
	}
	ctx := r.Context()
	if err := h.visible(ctx, p, in.AccountID); err != nil {
		h.writeErr(w, r, err)
		return
	}
	// rule: BIL-I-009
	// The imported statement's closing balance is the bank balance. A different
	// bank_balance_cents is refused before anything is prepared, never silently ignored.
	closing, err := h.Statements.Closing(ctx, in.AccountID, period)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if *in.BankBalanceCents != closing {
		h.fail(w, r, http.StatusUnprocessableEntity, CodeBankBalanceMismatch, "bank_balance_cents does not match the imported statement's closing balance")
		return
	}
	rec, err := h.Recons.Prepare(ctx, in.AccountID, period, nil, preparerOf(p))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, viewOf(rec, 1))
}

func parseMonth(s string) (Period, bool) {
	m := monthPattern.FindStringSubmatch(s)
	if m == nil {
		return Period{}, false
	}
	var y, mo int
	if _, err := fmt.Sscanf(m[1]+" "+m[2], "%d %d", &y, &mo); err != nil {
		return Period{}, false
	}
	return Period{Year: y, Month: time.Month(mo)}, true
}

// preparerOf is the user a request prepares for: the user, or the user an agent acts for.
func preparerOf(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}

// submit opens trust.reconciliation_signoff for a balanced, prepared reconciliation. It never
// approves: Approve and SignOff run only in the gate's effect.
func (h *WriteHTTP) submit(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	if body, err := io.ReadAll(r.Body); err != nil || strings.TrimSpace(string(body)) != "" {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Submit takes no body")
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "reconciliation_id")
	rec, err := h.Recons.Get(ctx, id)
	if err == nil {
		err = h.visible(ctx, p, rec.AccountID)
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	version, err := h.Versions.Version(ctx, id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if err := server.CheckIfMatch(r, version); err != nil {
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	}
	if rec.Status != StatusPrepared {
		h.fail(w, r, http.StatusConflict, CodePeriodLocked, "Reconciliation is already signed off")
		return
	}
	// rule: BIL-I-009
	// Refused unless the adjusted bank, book and client ledgers agree: every difference must
	// be explained by a reconciling item.
	if !rec.Result.Balanced() {
		h.fail(w, r, http.StatusUnprocessableEntity, CodeUnexplainedDifference, "Reconciliation has an unexplained difference")
		return
	}
	payload, err := json.Marshal(map[string]any{"reconciliation_id": id, "account_id": rec.AccountID,
		"month":               fmt.Sprintf("%04d-%02d", rec.Period.Year, int(rec.Period.Month)),
		"adjusted_bank_cents": rec.Result.AdjustedBankCents, "journal_balance_cents": rec.Result.BookCents,
		"client_listing_total_cents": rec.Result.ClientLedgerCents})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	var gid gate.GateID
	err = h.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		gid, err = h.Gates.Open(ctx, tx, GateSignOff, gate.Subject{Kind: SubjectReconciliation, ID: id}, payload, p)
		return err
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	opened, err := h.Gates.Get(ctx, gid)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, h.gateBody(opened, rec.AccountID))
}

// writeErr maps a service error to its problem. Error text never reaches the body.
func (h *WriteHTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
	case errors.Is(err, ErrUnverified):
		h.fail(w, r, http.StatusConflict, CodeUnverified, "Trust reconciliation rules are not verified")
	case errors.Is(err, ErrDuplicateStatement):
		h.fail(w, r, http.StatusConflict, CodeDuplicateStatement, "A statement for this account and month is already imported")
	case errors.Is(err, ErrPeriodLocked), errors.Is(err, ErrInvalidState):
		h.fail(w, r, http.StatusConflict, CodePeriodLocked, "The month is reconciled and locked")
	case errors.Is(err, ErrUnbalanced):
		h.fail(w, r, http.StatusUnprocessableEntity, CodeUnexplainedDifference, "Reconciliation has an unexplained difference")
	case errors.Is(err, ErrNoParser):
		h.fail(w, r, http.StatusUnprocessableEntity, CodeNoStatementParser, "No bank statement parser is configured")
	case errors.Is(err, ErrUnparseable), errors.Is(err, ErrStatementMismatch):
		h.fail(w, r, http.StatusUnprocessableEntity, CodeUnparseableStatement, "Statement cannot be read or does not sum to its closing balance")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

func viewOf(rec Reconciliation, version int64) reconView {
	v := reconView{ID: rec.ID, Version: version, AccountID: rec.AccountID,
		Month:            fmt.Sprintf("%04d-%02d", rec.Period.Year, int(rec.Period.Month)),
		BankBalanceCents: rec.Result.BankCents, OutstandingItemsCents: rec.Result.AdjustedBankCents - rec.Result.BankCents,
		AdjustedBankCents: rec.Result.AdjustedBankCents, JournalBalanceCents: rec.Result.BookCents,
		ClientListingTotalCents: rec.Result.ClientLedgerCents, Balanced: rec.Result.Balanced(), Status: "prepared"}
	if rec.Status == StatusApproved {
		v.Status, v.SignedBy = "signed", rec.ApprovedBy
		if rec.ApprovedAt != nil {
			v.SignedAt = rec.ApprovedAt.UTC().Format(time.RFC3339)
		}
	}
	return v
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

func (h *WriteHTTP) gateBody(g gate.Gate, accountID string) gateBody {
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
		Subject: map[string]any{"type": g.Subject.Kind, "id": g.Subject.ID, "account_id": accountID},
		Payload: g.Payload, PayloadHash: g.PayloadHash, Preparers: []actorBody{prep}, MinApprovers: min,
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
