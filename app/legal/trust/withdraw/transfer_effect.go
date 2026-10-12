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
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/registry"
)

// TransferPayload is the trust.transfer gate payload. The gate's payload hash covers every
// field, so the approval binds the transfer, both matters and the amount.
type TransferPayload struct {
	TransferID   string `json:"transfer_id"`
	FromMatterID string `json:"from_matter_id"`
	ToMatterID   string `json:"to_matter_id"`
	AmountCents  *int64 `json:"amount_cents"`
	Reason       string `json:"reason"`
}

// checkTerms fails closed unless the approved payload states h's terms exactly.
func (p TransferPayload) checkTerms(h HeldTransfer) error {
	// rule: ARC-I-019
	switch {
	case p.TransferID != h.ID:
		return fmt.Errorf("transfer id: %w", ErrTermsMismatch)
	case p.FromMatterID == "" || p.FromMatterID != h.FromMatterID:
		return fmt.Errorf("source matter: %w", ErrTermsMismatch)
	case p.ToMatterID == "" || p.ToMatterID != h.ToMatterID:
		return fmt.Errorf("destination matter: %w", ErrTermsMismatch)
	case p.AmountCents == nil || *p.AmountCents != h.AmountCents:
		return fmt.Errorf("amount: %w", ErrTermsMismatch)
	}
	return nil
}

// HeldTransfer is a trust.trust_transfer row locked for its effect.
type HeldTransfer struct {
	ID          string
	FromLedger  trust.LedgerID
	ToLedger    trust.LedgerID
	AccountID   trust.AccountID // the source ledger's trust account
	AmountCents int64
	// FromMatterID and ToMatterID are the matters of the source and destination ledgers.
	FromMatterID string
	ToMatterID   string
	Status       string // database status
}

// TransferGateStore keeps transfer state for the gate effect. Every method runs in the caller's tx.
type TransferGateStore interface {
	// LockTransfer reads the transfer FOR UPDATE, or returns ErrNotFound.
	LockTransfer(ctx context.Context, tx pgx.Tx, id string) (HeldTransfer, error)
	// MarkAuthorized moves a transfer awaiting authorisation to authorized under gateID, or
	// returns ErrNotAuthorized.
	MarkAuthorized(ctx context.Context, tx pgx.Tx, id, gateID string) error
}

// TransferEffect is the trust.transfer gate effect: in the gate's tx it authorises the
// transfer and posts it through Transfers.Post, which locks both ledgers and re-checks the
// source's available balance. Any error rolls the gate tx back, so nothing is posted.
type TransferEffect struct {
	Store     TransferGateStore
	Transfers *Transfers
	Signers   SignatoryCheck
	Now       func() time.Time
	// SoD applies the sole-practitioner exception; while nil, the preparer is always refused.
	SoD *gates.SoDGuard
}

// TransferEffects returns the effect handler for gates.RegisterKinds.
func TransferEffects(e *TransferEffect) map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{TransferGateKind: e.Apply}
}

// Apply is the trust.transfer EffectFunc.
func (e *TransferEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	var p TransferPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.TransferID == "" || p.Reason == "" {
		return failed(CodeNotAuthorized, fmt.Errorf("transfer payload: %w", ErrInvalid))
	}
	if len(g.Approvers) == 0 || e.Transfers == nil || e.Signers == nil {
		return failed(CodeNotAuthorized, ErrNotAuthorized)
	}
	approver := g.Approvers[len(g.Approvers)-1]
	// rule: ARC-I-027
	// The agent never approves; the preparer never approves their own transfer.
	if approver.Kind != registry.PrincipalUser || approver.UserID == "" {
		return failed(CodeNotAuthorized, ErrNotAuthorized)
	}
	preparer := g.Preparer.UserID
	if g.Preparer.Kind == registry.PrincipalAgent {
		preparer = g.Preparer.ActingFor
	}
	// rule: ARC-I-021
	if _, err := e.SoD.Approve(ctx, tx, g.GateID, gates.Duty{What: TransferGateKind, ObjectID: p.TransferID,
		Preparer: preparer, Approver: approver.UserID}); err != nil {
		return failed(CodeNotAuthorized, fmt.Errorf("%w: %w", ErrPreparerApproves, err))
	}
	h, err := e.Store.LockTransfer(ctx, tx, p.TransferID)
	if err != nil {
		return fmt.Errorf("transfer effect: %w", err)
	}
	if h.Status != "pending_authorization" {
		return failed(CodeNotAuthorized, fmt.Errorf("transfer status %s: %w", h.Status, ErrNotAuthorized))
	}
	// The approval binds the terms it was given: a gate approved for one amount or pair of
	// matters never moves another.
	if err := p.checkTerms(h); err != nil {
		return failed(CodeNotAuthorized, err)
	}
	ok, err := e.Signers.IsSignatory(ctx, tx, h.AccountID, approver.UserID)
	if err != nil {
		return fmt.Errorf("transfer effect: signatory: %w", err)
	}
	if !ok {
		return failed(CodeNotAuthorized, ErrSignerNotAuthorized)
	}
	if err := e.Store.MarkAuthorized(ctx, tx, h.ID, g.GateID); err != nil {
		return fmt.Errorf("transfer effect: %w", err)
	}
	loc := e.Transfers.Loc
	if loc == nil {
		return fmt.Errorf("transfer effect: no business time zone: %w", ErrInvalid)
	}
	day := clock.DateOf(e.Now(), loc).Start(loc)
	// rule: ARC-I-024
	_, err = e.Transfers.Post(ctx, tx, Transfer{
		ID: h.ID, FromLedger: h.FromLedger, ToLedger: h.ToLedger, AmountCents: h.AmountCents,
		Reason: p.Reason, ApprovalID: g.GateID, TxnDate: day, PostedBy: approver.UserID,
	})
	if errors.Is(err, ErrTrustOverdraw) {
		return failed(CodeInsufficientTrustFunds, err)
	}
	if err != nil {
		return fmt.Errorf("transfer effect: %w", err)
	}
	return nil
}

const lockTransferSQL = `SELECT x.id::text, x.from_ledger_id::text, x.to_ledger_id::text, l.account_id::text,
  x.amount_cents, x.status, l.matter_id::text, d.matter_id::text
FROM trust.trust_transfer x JOIN trust.client_ledger l ON l.id = x.from_ledger_id
  JOIN trust.client_ledger d ON d.id = x.to_ledger_id
WHERE x.id = $1::uuid FOR UPDATE OF x`

// LockTransfer implements TransferGateStore.
func (PGTransferStore) LockTransfer(ctx context.Context, tx pgx.Tx, id string) (HeldTransfer, error) {
	var h HeldTransfer
	var from, to, acct string
	err := tx.QueryRow(ctx, lockTransferSQL, id).Scan(&h.ID, &from, &to, &acct, &h.AmountCents, &h.Status,
		&h.FromMatterID, &h.ToMatterID)
	if err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
			return HeldTransfer{}, ErrNotFound
		}
		return HeldTransfer{}, fmt.Errorf("lock transfer: %w", err)
	}
	h.FromLedger, h.ToLedger, h.AccountID = trust.LedgerID(from), trust.LedgerID(to), trust.AccountID(acct)
	return h, nil
}

// MarkAuthorized implements TransferGateStore.
func (PGTransferStore) MarkAuthorized(ctx context.Context, tx pgx.Tx, id, gateID string) error {
	tag, err := tx.Exec(ctx, `UPDATE trust.trust_transfer SET status = 'authorized', approval_id = $2::uuid
WHERE id = $1::uuid AND status = 'pending_authorization'`, id, gateID)
	if err != nil {
		return fmt.Errorf("mark transfer authorized: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotAuthorized
	}
	return nil
}

var _ TransferGateStore = PGTransferStore{}
