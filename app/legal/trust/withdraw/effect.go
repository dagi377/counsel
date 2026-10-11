package withdraw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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

// EffectPayload is the trust.withdrawal gate payload.
type EffectPayload struct {
	WithdrawalID string `json:"withdrawal_id"`
}

// Held is a withdrawal locked for its effect, with the client ledger it draws on.
type Held struct {
	Withdrawal
	LedgerID trust.LedgerID
}

// EffectStore keeps withdrawal state. Every method runs in the caller's tx.
type EffectStore interface {
	// Lock reads the withdrawal FOR UPDATE, or returns ErrNotFound.
	Lock(ctx context.Context, tx pgx.Tx, id string) (Held, error)
	// MarkAuthorized records the authorisation; the withdrawal then holds its amount.
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
	// rule: ARC-I-027
	// The agent never approves; the preparer never approves their own withdrawal.
	if approver.Kind != registry.PrincipalUser || approver.UserID == "" {
		return failed(CodeNotAuthorized, ErrNotAuthorized)
	}
	if approver.UserID == h.Request.PreparedBy {
		return failed(CodeNotAuthorized, ErrPreparerApproves)
	}
	ok, err := e.Signers.IsSignatory(ctx, tx, trust.AccountID(h.Request.TrustAccountID), approver.UserID)
	if err != nil {
		return fmt.Errorf("withdrawal effect: signatory: %w", err)
	}
	if !ok {
		return failed(CodeNotAuthorized, ErrSignerNotAuthorized)
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
	if !h.ExpiresAt.IsZero() && !e.Now().Before(h.ExpiresAt) {
		return "", failed(CodeNotAuthorized, fmt.Errorf("authorization expired: %w", ErrNotAuthorized))
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
  coalesce(w.approval_id::text, ''), coalesce(w.authorized_by::text, ''), coalesce(w.executed_by::text, '')
FROM trust.withdrawal_request w JOIN trust.client_ledger l ON l.id = w.ledger_id
WHERE w.id = $1::uuid FOR UPDATE OF w`

// Lock implements EffectStore.
func (PGStore) Lock(ctx context.Context, tx pgx.Tx, id string) (Held, error) {
	var h Held
	var ledger, method, status string
	err := tx.QueryRow(ctx, lockWithdrawalSQL, id).Scan(&h.ID, &ledger, &h.Request.TrustAccountID, &h.Request.MatterID,
		&h.Request.AmountCents, &h.Request.Payee, &method, &h.Request.EFTAuthorizationID, &h.Request.Reason,
		&h.Request.PreparedBy, &status, &h.GateID, &h.AuthorizedBy, &h.ExecutedBy)
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
	return h, nil
}

// MarkAuthorized implements EffectStore.
func (PGStore) MarkAuthorized(ctx context.Context, tx pgx.Tx, id, by, gateID string, _ time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE trust.withdrawal_request SET status = 'authorized', authorized_by = $2::uuid,
  approval_id = $3::uuid WHERE id = $1::uuid`, id, by, gateID); err != nil {
		return fmt.Errorf("mark authorized: %w", err)
	}
	return nil
}

// MarkExecuted implements EffectStore.
func (PGStore) MarkExecuted(ctx context.Context, tx pgx.Tx, id, by string, on time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE trust.withdrawal_request SET status = 'executed', executed_by = $2::uuid,
  executed_on = $3::date WHERE id = $1::uuid`, id, by, on.Format(time.DateOnly)); err != nil {
		return fmt.Errorf("mark executed: %w", err)
	}
	return nil
}
