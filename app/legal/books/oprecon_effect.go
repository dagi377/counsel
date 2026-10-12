package books

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates/decider"
	"github.com/dagi377/counsel/kernel/registry"
)

// KindOperatingReconciliation is the G12g gate: approval signs off an operating bank
// reconciliation (gl.operating_reconciliation, 5700), mirroring the trust sign-off.
const KindOperatingReconciliation = "gl.operating_reconciliation"

// ErrReconMismatch means the reconciliation's figures differ from the approved ones, or the
// adjusted bank balance does not equal the book balance.
var ErrReconMismatch = errors.New("books: reconciliation figures do not agree")

// ErrReconNotFound means no operating reconciliation exists for the account and period end.
var ErrReconNotFound = errors.New("books: operating reconciliation not found")

// OpRecon is a gl.operating_reconciliation row, locked in the gate tx.
type OpRecon struct {
	ID                string
	PreparedBy        string
	Status            string // draft, ready, signed
	ApprovalID        string
	BankBalanceCents  int64
	AdjustedBankCents int64
	BookBalanceCents  int64
}

// OpReconStore keeps operating reconciliations. Every method runs in the gate tx.
// PGOpRecon implements it.
type OpReconStore interface {
	// LockRecon locks the account's reconciliation for periodEnd, or ErrReconNotFound.
	LockRecon(ctx context.Context, tx pgx.Tx, accountID, periodEnd string) (OpRecon, error)
	// SignRecon signs reconciliation id under gate gateID.
	SignRecon(ctx context.Context, tx pgx.Tx, id, by, gateID string, at time.Time) error
}

type opReconPayload struct {
	AccountID        string `json:"account_id"`
	PeriodEnd        string `json:"period_end"`
	BankBalanceCents *int64 `json:"bank_balance_cents"`
	BookBalanceCents *int64 `json:"book_balance_cents"`
}

// OpReconEffect is the gl.operating_reconciliation EffectFunc.
type OpReconEffect struct {
	Store OpReconStore
	Now   func() time.Time
}

// Apply signs the reconciliation. A row this gate already signed is a no-op.
func (e *OpReconEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	by, err := decider.Check(g)
	if err != nil {
		return fmt.Errorf("operating reconciliation: %w", err)
	}
	if e == nil || e.Store == nil || e.Now == nil {
		return fmt.Errorf("operating reconciliation %s: not wired: %w", g.GateID, ErrInvalid)
	}
	var p opReconPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.AccountID == "" || p.BankBalanceCents == nil || p.BookBalanceCents == nil {
		return fmt.Errorf("operating reconciliation %s: payload: %w", g.GateID, ErrInvalid)
	}
	if _, err := time.Parse(time.DateOnly, p.PeriodEnd); err != nil {
		return fmt.Errorf("operating reconciliation %s: period end: %w", g.GateID, ErrInvalid)
	}
	r, err := e.Store.LockRecon(ctx, tx, p.AccountID, p.PeriodEnd)
	if err != nil {
		return fmt.Errorf("operating reconciliation %s: %w", g.GateID, err)
	}
	// rule: ARC-I-012
	// Idempotent per gate.
	if r.Status == "signed" && r.ApprovalID == g.GateID {
		return nil
	}
	if r.Status == "signed" {
		return fmt.Errorf("operating reconciliation %s: signed under gate %s: %w", g.GateID, r.ApprovalID, ErrInvalid)
	}
	for _, a := range g.Approvers {
		// rule: ARC-I-021
		// The person who prepared the reconciliation never signs it.
		if a.UserID == r.PreparedBy {
			return fmt.Errorf("operating reconciliation %s: %w", g.GateID, decider.ErrPreparerApproves)
		}
	}
	// rule: BIL-I-009
	// The approval binds the figures it was shown, and only a reconciled month is signed.
	if *p.BankBalanceCents != r.BankBalanceCents || *p.BookBalanceCents != r.BookBalanceCents || r.AdjustedBankCents != r.BookBalanceCents {
		return fmt.Errorf("operating reconciliation %s: %w", g.GateID, ErrReconMismatch)
	}
	return e.Store.SignRecon(ctx, tx, r.ID, by, g.GateID, e.Now())
}

// PGOpRecon is OpReconStore over gl.operating_reconciliation.
type PGOpRecon struct{}

var _ OpReconStore = PGOpRecon{}

const lockOpReconSQL = `SELECT id::text, prepared_by::text, status, coalesce(approval_id::text, ''),
  bank_balance_cents, adjusted_bank_cents, book_balance_cents
FROM gl.operating_reconciliation WHERE account_id = $1::uuid AND period_end = $2::date FOR UPDATE`

const signOpReconSQL = `UPDATE gl.operating_reconciliation SET status = 'signed', signed_by = $2::uuid, signed_at = $4,
  approval_id = $3::uuid WHERE id = $1::uuid AND status IN ('draft','ready')`

// LockRecon implements OpReconStore.
func (PGOpRecon) LockRecon(ctx context.Context, tx pgx.Tx, accountID, periodEnd string) (OpRecon, error) {
	var r OpRecon
	err := tx.QueryRow(ctx, lockOpReconSQL, accountID, periodEnd).Scan(&r.ID, &r.PreparedBy, &r.Status, &r.ApprovalID,
		&r.BankBalanceCents, &r.AdjustedBankCents, &r.BookBalanceCents)
	if errors.Is(err, pgx.ErrNoRows) {
		return OpRecon{}, fmt.Errorf("operating reconciliation %s %s: %w", accountID, periodEnd, ErrReconNotFound)
	}
	if err != nil {
		return OpRecon{}, fmt.Errorf("operating reconciliation %s %s: %w", accountID, periodEnd, err)
	}
	return r, nil
}

// SignRecon implements OpReconStore.
func (PGOpRecon) SignRecon(ctx context.Context, tx pgx.Tx, id, by, gateID string, at time.Time) error {
	tag, err := tx.Exec(ctx, signOpReconSQL, id, by, gateID, at)
	if err != nil {
		return fmt.Errorf("sign operating reconciliation %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("sign operating reconciliation %s: %w", id, ErrInvalid)
	}
	return nil
}
