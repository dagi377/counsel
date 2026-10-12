package withdraw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateKind is the gate that authorises a trust withdrawal.
const GateKind = "trust.withdrawal"

// Effect failure codes, as the API reports them.
const (
	CodeInsufficientTrustFunds = "insufficient_trust_funds"
	CodeExecutorIsAuthorizer   = "executor_is_authorizer"
	CodeNotAuthorized          = "not_authorized"
	CodeUnclearedFunds         = "uncleared_funds"
)

// AuthorizationTTL is how long an authorised withdrawal may be executed; after it the
// authorisation has expired, Execute fails closed and the hold is released.
const AuthorizationTTL = 72 * time.Hour

var (
	// ErrAuthorizationExpired is returned when an authorisation is past its expiry (or has
	// none recorded). It matches ErrNotAuthorized.
	ErrAuthorizationExpired = fmt.Errorf("withdraw: authorization expired: %w", ErrNotAuthorized)
	// ErrTermsMismatch is returned when the withdrawal's amount, payee, method or matter differ
	// from the approved gate payload, or the payload does not state them. It matches
	// ErrNotAuthorized.
	ErrTermsMismatch = fmt.Errorf("withdraw: terms differ from the approved gate: %w", ErrNotAuthorized)
)

// AuditSoloException is the audit action recorded when a sole practitioner executes a
// withdrawal they authorised (ARC-I-027).
const AuditSoloException = "sod_solo_exception"

// EffectError is a gate effect that failed closed. It matches gate.ErrEffectFailed and the
// cause with errors.Is, and carries the API code.
type EffectError struct {
	Code string
	Err  error
}

func (e *EffectError) Error() string { return fmt.Sprintf("effect failed (%s): %v", e.Code, e.Err) }

// Unwrap lets errors.Is match both gate.ErrEffectFailed and the cause.
func (e *EffectError) Unwrap() []error { return []error{gate.ErrEffectFailed, e.Err} }

func failed(code string, err error) error { return &EffectError{Code: code, Err: err} }

// EffectPayload is the trust.withdrawal gate payload. The gate's payload hash covers every
// field, so the approval binds the withdrawal, its matter, amount, payee and method.
type EffectPayload struct {
	WithdrawalID string `json:"withdrawal_id"`
	MatterID     string `json:"matter_id"`
	AmountCents  *int64 `json:"amount_cents"`
	Payee        string `json:"payee"`
	Method       Method `json:"method"`
	// BillID is the bill a fee withdrawal pays; empty for any other withdrawal. Additive.
	BillID string `json:"bill_id,omitempty"`
}

// PayloadFor is the gate payload that approves exactly h's terms.
func PayloadFor(h Held) EffectPayload {
	amount := h.Request.AmountCents
	return EffectPayload{WithdrawalID: h.ID, MatterID: h.Request.MatterID, AmountCents: &amount,
		Payee: h.Request.Payee, Method: h.Request.Method, BillID: h.Request.BillID}
}

// checkTerms fails closed unless the approved payload states h's terms exactly.
func (p EffectPayload) checkTerms(h Held) error {
	// rule: ARC-I-019
	switch {
	case p.WithdrawalID == "" || p.WithdrawalID != h.ID:
		return fmt.Errorf("withdrawal id: %w", ErrTermsMismatch)
	case p.MatterID == "" || p.MatterID != h.Request.MatterID:
		return fmt.Errorf("matter: %w", ErrTermsMismatch)
	case p.AmountCents == nil || *p.AmountCents != h.Request.AmountCents:
		return fmt.Errorf("amount: %w", ErrTermsMismatch)
	case p.Payee == "" || p.Payee != h.Request.Payee:
		return fmt.Errorf("payee: %w", ErrTermsMismatch)
	case p.Method == "" || p.Method != h.Request.Method:
		return fmt.Errorf("method: %w", ErrTermsMismatch)
	case p.BillID != h.Request.BillID:
		return fmt.Errorf("bill: %w", ErrTermsMismatch)
	}
	return nil
}

// ApprovedPayloads reads the stored payload of an approved trust.withdrawal gate.
type ApprovedPayloads interface {
	// ApprovedPayload returns the payload of gateID when it is an approved trust.withdrawal
	// gate bound to withdrawalID, or ErrNotFound.
	ApprovedPayload(ctx context.Context, tx pgx.Tx, gateID, withdrawalID string) (json.RawMessage, error)
}

// Held is a withdrawal locked for its effect, with the client ledger it draws on.
type Held struct {
	Withdrawal
	LedgerID trust.LedgerID
}

// EffectStore keeps withdrawal state. Every method runs in the caller's tx.
type EffectStore interface {
	// Lock reads the withdrawal FOR UPDATE, or returns ErrNotFound. An authorised withdrawal
	// whose authorisation has expired returns ErrAuthorizationExpired.
	Lock(ctx context.Context, tx pgx.Tx, id string) (Held, error)
	// MarkAuthorized records the authorisation at at, expiring AuthorizationTTL later; the
	// withdrawal then holds its amount until it is executed or the authorisation expires.
	MarkAuthorized(ctx context.Context, tx pgx.Tx, id, by, gateID string, at time.Time) error
	// MarkExecuted records who executed the withdrawal at the bank and on which date.
	MarkExecuted(ctx context.Context, tx pgx.Tx, id, by string, on time.Time) error
}

// TrustLedgers is the slice of the trust ledger the effect uses; trust.LedgerStore and
// trustfake implement it. Both methods run in the caller's tx.
type TrustLedgers interface {
	// LockLedger locks the client ledger row FOR UPDATE in tx (the lock Disburse takes) and
	// returns its balance and the sum of its authorised-not-executed withdrawal holds.
	LockLedger(ctx context.Context, tx pgx.Tx, id trust.LedgerID) (trust.Balance, int64, error)
	Disburse(ctx context.Context, tx pgx.Tx, d trust.Disbursement) (trust.TxnID, error)
}

// SignatoryCheck reports whether userID is an active signatory on a trust account.
type SignatoryCheck interface {
	IsSignatory(ctx context.Context, tx pgx.Tx, account trust.AccountID, userID string) (bool, error)
}

// FeeBills reads the bill a fee withdrawal pays, in the caller's tx: its matter, delivery
// date and unpaid balance (net of fee transfers already reserved). The billing module owns
// the bill; boot wires the adapter.
type FeeBills interface {
	FeeBill(ctx context.Context, tx pgx.Tx, billID string) (FeeBill, error)
}

// FirmPolicy reads core.firm.sole_practitioner.
type FirmPolicy interface {
	SolePractitioner(ctx context.Context) (bool, error)
}

// Auditor records an audit action on a withdrawal inside tx.
type Auditor interface {
	Record(ctx context.Context, tx pgx.Tx, action, withdrawalID, by string) error
}

// Effect moves trust money out only through the trust.withdrawal gate: Apply is the gate's
// effect (authorisation by a signatory, never the preparer or an agent), and Execute posts
// the disbursement when a different user records the bank payment. Both run in the caller's
// tx and fail closed: on any error nothing is applied.
type Effect struct {
	Ledgers TrustLedgers
	Store   EffectStore
	Signers SignatoryCheck
	Firm    FirmPolicy
	Audit   Auditor
	Now     func() time.Time
	// EFT reads Form 9A records; while nil, every EFT execution fails closed.
	EFT EFTRecords
	// Approvals reads the approved gate payload at execution; while nil, every execution
	// fails closed.
	Approvals ApprovedPayloads
	// Bills reads the bill a fee withdrawal pays; while nil, every fee withdrawal fails
	// closed at authorization.
	Bills FeeBills
	// SoD applies the sole-practitioner exception when the preparer authorises their own
	// withdrawal; while nil, the preparer is always refused.
	SoD *gates.SoDGuard
}

// Apply is the trust.withdrawal EffectFunc. It runs in the gate's tx: it locks the withdrawal,
// then the client ledger (the order Execute and Disburse use), re-checks the available balance
// net of the authorised holds already on the ledger and marks the withdrawal authorised, which
// holds its amount. The ledger lock is held until tx ends, so a concurrent authorisation on the
// same ledger waits and then sees this hold.
func (e *Effect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	var p EffectPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.WithdrawalID == "" {
		return failed(CodeNotAuthorized, fmt.Errorf("payload: %w", ErrInvalid))
	}
	if len(g.Approvers) == 0 {
		return failed(CodeNotAuthorized, ErrNotAuthorized)
	}
	approver := g.Approvers[len(g.Approvers)-1]
	h, err := e.Store.Lock(ctx, tx, p.WithdrawalID)
	if err != nil {
		return fmt.Errorf("withdrawal effect: %w", err)
	}
	if h.Status != StatusRequested {
		return failed(CodeNotAuthorized, fmt.Errorf("status %s: %w", h.Status, ErrNotAuthorized))
	}
	// The approval binds the terms it was given: a gate approved at one amount, payee or
	// method never authorises another.
	if err := p.checkTerms(h); err != nil {
		return failed(CodeNotAuthorized, err)
	}
	// rule: ARC-I-027
	// The agent never approves; the preparer never approves their own withdrawal.
	if approver.Kind != registry.PrincipalUser || approver.UserID == "" {
		return failed(CodeNotAuthorized, ErrNotAuthorized)
	}
	// rule: ARC-I-021
	// A sole practitioner may authorise their own withdrawal with step-up and a stated
	// reason, audited as sole_practitioner.
	if _, err := e.SoD.Approve(ctx, tx, g.GateID, gates.Duty{What: GateKind, ObjectID: h.ID,
		Preparer: h.Request.PreparedBy, Approver: approver.UserID}); err != nil {
		return failed(CodeNotAuthorized, fmt.Errorf("%w: %w", ErrPreparerApproves, err))
	}
	ok, err := e.Signers.IsSignatory(ctx, tx, trust.AccountID(h.Request.TrustAccountID), approver.UserID)
	if err != nil {
		return fmt.Errorf("withdrawal effect: signatory: %w", err)
	}
	if !ok {
		return failed(CodeNotAuthorized, ErrSignerNotAuthorized)
	}
	// rule: BIL-L-035 (verify)
	// rule: BIL-L-037 (verify)
	// Money for fees leaves trust only for a delivered bill on this matter, and for no more
	// than its unpaid balance (By-Law 9 s.9(1)); re-checked here, at authorization, whatever
	// the request path checked.
	if NeedsFeeBill(h.Request) {
		if e.Bills == nil {
			return failed(CodeNotAuthorized, fmt.Errorf("fee bill: no bill reader: %w", ErrNotAuthorized))
		}
		if h.Request.BillID == "" {
			return failed(CodeNotAuthorized, fmt.Errorf("fee withdrawal names no bill: %w", ErrBillMatter))
		}
		b, err := e.Bills.FeeBill(ctx, tx, h.Request.BillID)
		if err != nil {
			return fmt.Errorf("withdrawal effect: fee bill: %w", err)
		}
		if err := CheckFeeBill(h.Request, b, e.Now()); err != nil {
			return failed(CodeNotAuthorized, err)
		}
	}
	// Expired authorisations on this ledger can never execute: release their holds first so
	// they do not keep the client's money out of reach.
	if r, ok := e.Store.(ledgerExpiryReleaser); ok {
		if _, err := r.ReleaseExpiredOnLedger(ctx, tx, h.LedgerID, e.Now()); err != nil {
			return fmt.Errorf("withdrawal effect: %w", err)
		}
	}
	// rule: ARC-I-024
	// rule: ARC-I-025
	// Lock the ledger before reading it: without the lock two authorisations could both read
	// the same available balance and together overdraw the client's trust money.
	b, held, err := e.Ledgers.LockLedger(ctx, tx, h.LedgerID)
	if err != nil {
		return fmt.Errorf("withdrawal effect: lock ledger: %w", err)
	}
	avail := trust.AvailableCents(b.ClearedCents, b.ProtectedCents, b.MinimumCents, held)
	if h.Request.AmountCents > avail {
		return failed(CodeInsufficientTrustFunds, fmt.Errorf("available %d, requested %d: %w",
			avail, h.Request.AmountCents, ErrTrustOverdraw))
	}
	// The hold is recorded in tx, under the ledger lock, so the next authorisation sees it.
	if err := e.Store.MarkAuthorized(ctx, tx, h.ID, approver.UserID, g.GateID, e.Now()); err != nil {
		return fmt.Errorf("withdrawal effect: %w", err)
	}
	return nil
}

// Execute records the bank payment of an authorised withdrawal and posts its disbursement in
// tx. The executor is a user other than the authoriser unless the firm is a sole practice
// (audited). The ledger is locked and its balance re-checked at posting; a shortfall fails
// closed with insufficient_trust_funds and posts nothing.
func (e *Effect) Execute(ctx context.Context, tx pgx.Tx, id string, by registry.Principal, executedOn time.Time) (trust.TxnID, error) {
	if by.Kind != registry.PrincipalUser || by.UserID == "" || executedOn.IsZero() {
		return "", failed(CodeNotAuthorized, ErrNotAuthorized)
	}
	h, err := e.Store.Lock(ctx, tx, id)
	if err != nil {
		return "", fmt.Errorf("execute withdrawal: %w", err)
	}
	if h.Status != StatusAuthorized || h.GateID == "" {
		return "", failed(CodeNotAuthorized, fmt.Errorf("status %s: %w", h.Status, ErrNotAuthorized))
	}
	// An authorisation with no recorded expiry is treated as expired: fail closed.
	if h.ExpiresAt.IsZero() || !e.Now().Before(h.ExpiresAt) {
		return "", failed(CodeNotAuthorized, ErrAuthorizationExpired)
	}
	// Re-check the terms against the approved payload: the row posts only what was approved.
	if e.Approvals == nil {
		return "", failed(CodeNotAuthorized, fmt.Errorf("no approved payload source: %w", ErrTermsMismatch))
	}
	raw, err := e.Approvals.ApprovedPayload(ctx, tx, h.GateID, h.ID)
	if errors.Is(err, ErrNotFound) {
		return "", failed(CodeNotAuthorized, fmt.Errorf("approved payload missing: %w", ErrTermsMismatch))
	}
	if err != nil {
		return "", fmt.Errorf("execute withdrawal: approved payload: %w", err)
	}
	var p EffectPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", failed(CodeNotAuthorized, fmt.Errorf("approved payload: %w", ErrTermsMismatch))
	}
	if err := p.checkTerms(h); err != nil {
		return "", failed(CodeNotAuthorized, err)
	}
	// rule: ARC-I-027
	if by.UserID == h.AuthorizedBy {
		solo, err := e.Firm.SolePractitioner(ctx)
		if err != nil {
			return "", fmt.Errorf("execute withdrawal: firm: %w", err)
		}
		if !solo {
			return "", failed(CodeExecutorIsAuthorizer, ErrNotAuthorized)
		}
		if err := e.Audit.Record(ctx, tx, AuditSoloException, h.ID, by.UserID); err != nil {
			return "", fmt.Errorf("execute withdrawal: audit: %w", err)
		}
	}
	// An EFT leaves trust only against its signed Form 9A record.
	if err := CheckEFT(ctx, tx, e.EFT, h, e.Now()); err != nil {
		if errors.Is(err, ErrNoEFTAuthorization) {
			return "", failed(CodeNoEFTAuthorization, err)
		}
		return "", fmt.Errorf("execute withdrawal: %w", err)
	}
	// rule: ARC-I-024
	// Disburse locks the client ledger and re-checks the balance under the lock.
	txn, err := e.Ledgers.Disburse(ctx, tx, trust.Disbursement{
		LedgerID: h.LedgerID, WithdrawalID: h.ID, ApprovalID: h.GateID, TxnDate: executedOn,
		Method: trust.Method(h.Request.Method), Counterparty: h.Request.Payee, AmountCents: h.Request.AmountCents,
		Purpose: h.Request.Reason, PostedBy: by.UserID,
	})
	if errors.Is(err, trust.ErrInsufficientFunds) {
		return "", failed(CodeInsufficientTrustFunds, fmt.Errorf("%w: %w", ErrTrustOverdraw, err))
	}
	if errors.Is(err, trust.ErrUnclearedFunds) {
		return "", failed(CodeUnclearedFunds, fmt.Errorf("%w: %w", ErrUnclearedFunds, err))
	}
	if err != nil {
		return "", fmt.Errorf("execute withdrawal: %w", err)
	}
	if err := e.Store.MarkExecuted(ctx, tx, h.ID, by.UserID, executedOn); err != nil {
		return "", fmt.Errorf("execute withdrawal: %w", err)
	}
	return txn, nil
}

// PGStore is EffectStore over trust.withdrawal_request.
type PGStore struct{}

// Database statuses map onto the contract's: proposed and pending_authorization are both
// "requested".
const lockWithdrawalSQL = `SELECT w.id::text, w.ledger_id::text, l.account_id::text, l.matter_id::text, w.amount_cents,
  w.payee, w.method, coalesce(w.form_9a_doc_id::text, ''), w.purpose_category, w.initiated_by::text, w.status,
  coalesce(w.approval_id::text, ''), coalesce(w.authorized_by::text, ''), coalesce(w.executed_by::text, ''),
  w.authorized_at, w.expires_at, now(), coalesce(w.bill_id::text, '')
FROM trust.withdrawal_request w JOIN trust.client_ledger l ON l.id = w.ledger_id
WHERE w.id = $1::uuid FOR UPDATE OF w`

// Lock implements EffectStore.
func (PGStore) Lock(ctx context.Context, tx pgx.Tx, id string) (Held, error) {
	var h Held
	var ledger, method, status string
	var authorizedAt, expiresAt *time.Time
	var dbNow time.Time
	err := tx.QueryRow(ctx, lockWithdrawalSQL, id).Scan(&h.ID, &ledger, &h.Request.TrustAccountID, &h.Request.MatterID,
		&h.Request.AmountCents, &h.Request.Payee, &method, &h.Request.EFTAuthorizationID, &h.Request.Reason,
		&h.Request.PreparedBy, &status, &h.GateID, &h.AuthorizedBy, &h.ExecutedBy, &authorizedAt, &expiresAt, &dbNow,
		&h.Request.BillID)
	if err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
			return Held{}, ErrNotFound
		}
		return Held{}, fmt.Errorf("lock withdrawal: %w", err)
	}
	h.LedgerID, h.Request.Method = trust.LedgerID(ledger), Method(method)
	switch status {
	case "proposed", "pending_authorization":
		h.Status = StatusRequested
	case "authorized":
		h.Status = StatusAuthorized
	case "executed", "applied", "confirmed":
		h.Status = StatusExecuted
	default:
		h.Status = StatusRejected
	}
	if authorizedAt != nil {
		h.AuthorizedAt = *authorizedAt
	}
	if expiresAt != nil {
		h.ExpiresAt = *expiresAt
	}
	// rule: ARC-I-012
	// An authorisation is executable only until it expires; one with no expiry recorded
	// (authorised before expiry was stored) has expired.
	if h.Status == StatusAuthorized && (h.ExpiresAt.IsZero() || !dbNow.Before(h.ExpiresAt)) {
		return Held{}, failed(CodeNotAuthorized, fmt.Errorf("withdrawal %s: %w", h.ID, ErrAuthorizationExpired))
	}
	return h, nil
}

// MarkAuthorized implements EffectStore.
func (PGStore) MarkAuthorized(ctx context.Context, tx pgx.Tx, id, by, gateID string, at time.Time) error {
	if at.IsZero() {
		return fmt.Errorf("mark authorized: no time: %w", ErrInvalid)
	}
	if _, err := tx.Exec(ctx, markAuthorizedSQL, id, by, gateID, at, at.Add(AuthorizationTTL)); err != nil {
		return fmt.Errorf("mark authorized: %w", err)
	}
	return nil
}

const markAuthorizedSQL = `UPDATE trust.withdrawal_request SET status = 'authorized', authorized_by = $2::uuid,
  approval_id = $3::uuid, authorized_at = $4, expires_at = $5 WHERE id = $1::uuid`

// releaseExpiredSQL cancels authorisations that have expired (or have no expiry recorded),
// which drops their hold from trust.available_cents and the ledger sums.
const releaseExpiredSQL = `UPDATE trust.withdrawal_request SET status = 'cancelled'
WHERE status = 'authorized' AND (expires_at IS NULL OR expires_at <= $1)`

// ledgerExpiryReleaser is an EffectStore that can release expired holds on one ledger.
type ledgerExpiryReleaser interface {
	ReleaseExpiredOnLedger(ctx context.Context, tx pgx.Tx, ledger trust.LedgerID, now time.Time) (int64, error)
}

var _ ledgerExpiryReleaser = PGStore{}

// ReleaseExpiredOnLedger cancels the authorisations on one ledger that expired at now.
func (PGStore) ReleaseExpiredOnLedger(ctx context.Context, tx pgx.Tx, ledger trust.LedgerID, now time.Time) (int64, error) {
	tag, err := tx.Exec(ctx, releaseExpiredSQL+` AND ledger_id = $2::uuid`, now, string(ledger))
	if err != nil {
		return 0, fmt.Errorf("release expired withdrawals: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ReleaseExpired cancels every authorisation expired at now and returns how many it released.
// The withdrawal expiry job runs it; an expired authorisation can never be executed, so its
// hold must not keep the client's money out of reach.
func (PGStore) ReleaseExpired(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error) {
	tag, err := tx.Exec(ctx, releaseExpiredSQL, now)
	if err != nil {
		return 0, fmt.Errorf("release expired withdrawals: %w", err)
	}
	return tag.RowsAffected(), nil
}

// PGApprovals reads approved trust.withdrawal gate payloads from the stored JCS bytes. A gate
// with no stored payload is not evidence of the approved terms, so it reads as ErrNotFound.
type PGApprovals struct{}

var _ ApprovedPayloads = PGApprovals{}

const approvedPayloadSQL = `SELECT payload_jcs FROM gate.approval_request
WHERE id = $1::uuid AND (gate_code = $2 OR gate_code = 'billing.trust_fee_withdrawal') AND status = 'approved' AND payload_id = $3::uuid AND payload_jcs IS NOT NULL`

// ApprovedPayload implements ApprovedPayloads.
func (PGApprovals) ApprovedPayload(ctx context.Context, tx pgx.Tx, gateID, withdrawalID string) (json.RawMessage, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, approvedPayloadSQL, gateID, GateKind, withdrawalID).Scan(&raw); err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("approved payload: %w", err)
	}
	return raw, nil
}

// MarkExecuted implements EffectStore.
func (PGStore) MarkExecuted(ctx context.Context, tx pgx.Tx, id, by string, on time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE trust.withdrawal_request SET status = 'executed', executed_by = $2::uuid,
  executed_on = $3::date WHERE id = $1::uuid`, id, by, on.Format(time.DateOnly)); err != nil {
		return fmt.Errorf("mark executed: %w", err)
	}
	return nil
}
