package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/gate"
)

// ErrNoTrustLedger is returned when a trust refund's account and matter name no single
// client ledger.
var ErrNoTrustLedger = errors.New("payment: no single trust ledger for refund")

// PGRefunds is RefundStore over billing.payment, billing.refund and billing.client_credit.
type PGRefunds struct{}

var _ RefundStore = PGRefunds{}

// refundedStatuses are the billing.refund statuses that count against a payment.
const refundedStatuses = `('approved','sent')`

// lockRefundableSQL reads the payment FOR UPDATE with what is already refunded and, for a
// trust deposit, the matter of the client ledger it was received into.
const lockRefundableSQL = `SELECT p.id::text, coalesce(p.payer_contact_id::text, ''), p.amount_cents, p.deposit_to,
  coalesce(p.trust_account_id::text, ''),
  coalesce((SELECT sum(r.amount_cents) FROM billing.refund r
    WHERE r.payment_id = p.id AND r.status IN ` + refundedStatuses + `), 0)::bigint,
  coalesce((SELECT l.matter_id::text FROM trust.trust_transaction t JOIN trust.client_ledger l ON l.id = t.ledger_id
    WHERE t.payment_id = p.id ORDER BY t.id LIMIT 1), '')
FROM billing.payment p WHERE p.id = $1::uuid FOR UPDATE OF p`

// LockRefundable implements RefundStore.
func (PGRefunds) LockRefundable(ctx context.Context, tx pgx.Tx, id string) (Refundable, error) {
	var r Refundable
	err := tx.QueryRow(ctx, lockRefundableSQL, id).Scan(&r.PaymentID, &r.ClientID, &r.AmountCents, &r.DepositTo,
		&r.TrustAccountID, &r.RefundedCents, &r.MatterID)
	if missing(err) {
		return Refundable{}, fmt.Errorf("lock refundable %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Refundable{}, fmt.Errorf("lock refundable %s: %w", id, err)
	}
	return r, nil
}

// RefundByApproval implements RefundStore.
func (PGRefunds) RefundByApproval(ctx context.Context, tx pgx.Tx, approvalID string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM billing.refund WHERE approval_id = $1::uuid ORDER BY id LIMIT 1`, approvalID).Scan(&id)
	if missing(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("refund by approval %s: %w", approvalID, err)
	}
	return id, nil
}

const insertRefundSQL = `INSERT INTO billing.refund (payment_id, amount_cents, reason, approval_id, trust_withdrawal_id, status)
VALUES ($1::uuid, $2, $3, $4::uuid, NULLIF($5, '')::uuid, 'approved') RETURNING id::text`

// refundCreditSQL records an operating refund as client credit used (a negative row).
const refundCreditSQL = `INSERT INTO billing.client_credit (client_id, amount_cents, source_type, source_id, created_by)
VALUES ($1::uuid, $2, 'refund', $3::uuid, $4::uuid)`

// refundStatusSQL moves the payment to refunded when nothing is left of it, else partially_refunded.
const refundStatusSQL = `UPDATE billing.payment p SET status = CASE
  WHEN (SELECT coalesce(sum(r.amount_cents), 0) FROM billing.refund r
        WHERE r.payment_id = p.id AND r.status IN ` + refundedStatuses + `) >= p.amount_cents
  THEN 'refunded' ELSE 'partially_refunded' END
WHERE p.id = $1::uuid`

// RecordRefund implements RefundStore.
func (PGRefunds) RecordRefund(ctx context.Context, tx pgx.Tx, r RefundRecord) (string, error) {
	// rule: BIL-C-064
	// Integer cents only; a refund is never zero or negative.
	if r.PaymentID == "" || r.AmountCents <= 0 || r.ApprovalID == "" || r.ApprovedBy == "" {
		return "", fmt.Errorf("record refund: %w", ErrInvalid)
	}
	var id string
	if err := tx.QueryRow(ctx, insertRefundSQL, r.PaymentID, r.AmountCents, r.Reason, r.ApprovalID,
		r.TrustWithdrawalID).Scan(&id); err != nil {
		return "", fmt.Errorf("record refund: %w", err)
	}
	if r.TrustWithdrawalID == "" {
		if r.ClientID == "" {
			return "", fmt.Errorf("record refund %s: operating refund without client: %w", id, ErrInvalid)
		}
		if _, err := tx.Exec(ctx, refundCreditSQL, r.ClientID, -r.AmountCents, id, r.ApprovedBy); err != nil {
			return "", fmt.Errorf("record refund %s: client credit: %w", id, err)
		}
	}
	if _, err := tx.Exec(ctx, refundStatusSQL, r.PaymentID); err != nil {
		return "", fmt.Errorf("record refund %s: payment status: %w", id, err)
	}
	return id, nil
}

// LedgerLocker locks a client ledger and returns its balance and authorised holds;
// trust.LedgerStore implements it.
type LedgerLocker interface {
	LockLedger(ctx context.Context, tx pgx.Tx, id trust.LedgerID) (trust.Balance, int64, error)
}

// WithdrawalGates opens the trust.withdrawal gate in the caller's tx.
type WithdrawalGates interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error)
}

// PGTrustRefunds is TrustRefunds over trust.client_ledger and trust.withdrawal_request: a
// trust refund becomes a client_refund withdrawal awaiting the trust.withdrawal gate, under
// the same rules and gate as any other withdrawal.
type PGTrustRefunds struct {
	Ledgers LedgerLocker
	Gates   WithdrawalGates
}

var _ TrustRefunds = PGTrustRefunds{}

const refundLedgerSQL = `SELECT id::text FROM trust.client_ledger WHERE account_id = $1::uuid AND matter_id = $2::uuid LIMIT 2`

// ledgerOf returns the one client ledger of account and matter.
func ledgerOf(ctx context.Context, tx pgx.Tx, account, matter string) (trust.LedgerID, error) {
	rows, err := tx.Query(ctx, refundLedgerSQL, account, matter)
	if err != nil {
		return "", fmt.Errorf("refund ledger: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", fmt.Errorf("refund ledger: %w", err)
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("refund ledger: %d ledgers: %w", len(ids), ErrNoTrustLedger)
	}
	return trust.LedgerID(ids[0]), nil
}

// Funds implements TrustRefunds: it locks the ledger (the lock the withdrawal effect takes)
// and reports its position.
func (t PGTrustRefunds) Funds(ctx context.Context, tx pgx.Tx, trustAccountID, matterID string) (withdraw.Funds, error) {
	if t.Ledgers == nil {
		return withdraw.Funds{}, fmt.Errorf("trust refund funds: no ledgers: %w", ErrInvalid)
	}
	id, err := ledgerOf(ctx, tx, trustAccountID, matterID)
	if err != nil {
		return withdraw.Funds{}, err
	}
	b, held, err := t.Ledgers.LockLedger(ctx, tx, id)
	if err != nil {
		return withdraw.Funds{}, fmt.Errorf("trust refund funds: %w", err)
	}
	return FundsOf(b, held), nil
}

// FundsOf maps a locked ledger balance and its authorised holds to withdraw.Funds. All cents.
func FundsOf(b trust.Balance, heldCents int64) withdraw.Funds {
	return withdraw.Funds{
		ClearedCents: b.ClearedCents, UnclearedCents: max(b.PostedCents-b.ClearedCents, 0),
		ProtectedCents: b.ProtectedCents, MinimumCents: b.MinimumCents, AuthorizedCents: heldCents,
	}
}

// withdrawalPayload is the trust.withdrawal gate payload (spec/gates.json) of a refund.
type withdrawalPayload struct {
	WithdrawalID    string `json:"withdrawal_id"`
	MatterID        string `json:"matter_id"`
	AmountCents     int64  `json:"amount_cents"`
	PurposeCategory string `json:"purpose_category"`
	Payee           string `json:"payee"`
	Method          string `json:"method"`
}

// refundPurposeGate and refundPurposeDB are a client refund's purpose in the gate payload
// and in trust.withdrawal_request.
const (
	refundPurposeGate = "refund"
	refundPurposeDB   = "client_refund"
)

const insertRefundWithdrawalSQL = `INSERT INTO trust.withdrawal_request
  (ledger_id, purpose_category, payee, amount_cents, method, initiated_by, status)
VALUES ($1::uuid, '` + refundPurposeDB + `', $2, $3, $4, $5::uuid, 'pending_authorization') RETURNING id::text`

// priorRefundWithdrawalSQL finds the withdrawal an earlier run under the same key recorded.
const priorRefundWithdrawalSQL = `SELECT coalesce(trust_withdrawal_id::text, '') FROM billing.refund
WHERE 'refund:' || approval_id::text = $1 AND trust_withdrawal_id IS NOT NULL ORDER BY id LIMIT 1`

// Request implements TrustRefunds: it records the validated withdrawal and opens its
// trust.withdrawal gate, prepared by r.PreparedBy. The same IdempotencyKey returns the
// first withdrawal.
func (t PGTrustRefunds) Request(ctx context.Context, tx pgx.Tx, r withdraw.Request) (string, error) {
	if t.Gates == nil || r.IdempotencyKey == "" || r.PreparedBy == "" || r.AmountCents <= 0 {
		return "", fmt.Errorf("trust refund request: %w", ErrInvalid)
	}
	var prior string
	err := tx.QueryRow(ctx, priorRefundWithdrawalSQL, r.IdempotencyKey).Scan(&prior)
	switch {
	case err == nil && prior != "":
		return prior, nil
	case err != nil && !missing(err):
		return "", fmt.Errorf("trust refund request: %w", err)
	}
	ledger, err := ledgerOf(ctx, tx, r.TrustAccountID, r.MatterID)
	if err != nil {
		return "", err
	}
	var id string
	if err := tx.QueryRow(ctx, insertRefundWithdrawalSQL, string(ledger), r.Payee, r.AmountCents, string(r.Method),
		r.PreparedBy).Scan(&id); err != nil {
		return "", fmt.Errorf("trust refund request: %w", err)
	}
	payload, err := json.Marshal(withdrawalPayload{WithdrawalID: id, MatterID: r.MatterID, AmountCents: r.AmountCents,
		PurposeCategory: refundPurposeGate, Payee: r.Payee, Method: string(r.Method)})
	if err != nil {
		return "", fmt.Errorf("trust refund request: payload: %w", err)
	}
	// rule: ARC-I-019
	// The withdrawal is prepared by the refund's approver; a different signatory authorises it.
	if _, err := t.Gates.Open(ctx, tx, withdraw.GateKind, gate.Subject{Kind: withdraw.Subject, ID: id}, payload,
		gate.Principal{Kind: gate.PrincipalUser, UserID: r.PreparedBy}); err != nil {
		return "", fmt.Errorf("trust refund request: open gate: %w", err)
	}
	return id, nil
}

// missing reports a missing row or a malformed id.
func missing(err error) bool {
	var pg *pgconn.PgError
	return errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02")
}
