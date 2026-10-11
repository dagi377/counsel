package withdraw

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/gate"
)

// TransferRequest asks for a ledger-to-ledger transfer between two matters on one trust
// account. Money is integer cents.
type TransferRequest struct {
	TrustAccountID string
	FromMatterID   string
	ToMatterID     string
	AmountCents    int64
	Reason         string
	// PreparedBy is the person who prepares the transfer: for an agent run, the user it acts
	// for. It is stored on the row and never authorizes the transfer.
	PreparedBy string
	// Preparer is the gate's preparer: the user PreparedBy, or the agent run acting for them.
	Preparer gate.Principal
	// Subject is the gate's subject. Empty uses the transfer row (SubjectTransfer).
	Subject gate.Subject
}

// TransferRequested is a recorded transfer awaiting authorization and its open gate.
type TransferRequested struct {
	ID         string
	GateID     string
	FromLedger trust.LedgerID
	ToLedger   trust.LedgerID
}

// SubjectTransfer is the gate subject kind of a trust.trust_transfer row.
const SubjectTransfer = "trust.trust_transfer"

// TransferRequestStore keeps transfer requests. Every method runs in the caller's tx.
type TransferRequestStore interface {
	// LedgerOf returns the matter's client ledger on the account, or ErrNotFound.
	LedgerOf(ctx context.Context, tx pgx.Tx, account, matter string) (trust.LedgerID, error)
	// LockLedgers locks both ledgers in id order (TransferStore.LockLedgers).
	LockLedgers(ctx context.Context, tx pgx.Tx, a, b trust.LedgerID) ([]trust.Ledger, error)
	// Available is the ledger's available balance net of authorised holds.
	Available(ctx context.Context, tx pgx.Tx, id trust.LedgerID, exceptTransfer string) (int64, error)
	// InsertTransfer records the transfer as pending_authorization and returns its id.
	InsertTransfer(ctx context.Context, tx pgx.Tx, from, to trust.LedgerID, r TransferRequest) (string, error)
	// RecordGate stores the transfer's trust.transfer gate id.
	RecordGate(ctx context.Context, tx pgx.Tx, id, gateID string) error
}

// TransferRequests records transfer requests. It never authorizes or posts: the
// trust.transfer gate's effect (TransferEffect) does, for a signatory other than the preparer.
type TransferRequests struct {
	DB    TxRunner
	Store TransferRequestStore
	Gates Gates
}

func validateTransferRequest(r TransferRequest) error {
	if r.TrustAccountID == "" || r.FromMatterID == "" || r.ToMatterID == "" || r.FromMatterID == r.ToMatterID ||
		r.AmountCents <= 0 || r.Reason == "" {
		return fmt.Errorf("transfer request: %w", ErrInvalid)
	}
	// rule: ARC-I-027
	// The preparer is a person, and the gate's preparer is that person or a run acting for
	// them, so the effect can refuse the preparer as approver.
	switch {
	case r.PreparedBy == "":
		return fmt.Errorf("transfer request: no preparer: %w", ErrInvalid)
	case r.Preparer.Kind == gate.PrincipalUser && r.Preparer.UserID == r.PreparedBy:
	case r.Preparer.Kind == gate.PrincipalAgent && r.Preparer.ActingFor == r.PreparedBy:
	default:
		return fmt.Errorf("transfer request: gate preparer: %w", ErrInvalid)
	}
	return nil
}

// Request records r as pending authorization and opens its trust.transfer gate in one tx:
// either both exist or neither does. Under the ledger locks it refuses an amount above the
// source ledger's available balance (By-Law 9). Recording moves no money, so it needs no
// flag: the effect's Transfers.Post fails closed while FlagBylaw9Verified is off.
func (s TransferRequests) Request(ctx context.Context, r TransferRequest) (TransferRequested, error) {
	if s.DB == nil || s.Store == nil || s.Gates == nil {
		return TransferRequested{}, fmt.Errorf("transfer request: DB, Store and Gates: %w", ErrInvalid)
	}
	if err := validateTransferRequest(r); err != nil {
		return TransferRequested{}, err
	}
	var out TransferRequested
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		from, err := s.Store.LedgerOf(ctx, tx, r.TrustAccountID, r.FromMatterID)
		if err != nil {
			return fmt.Errorf("source ledger: %w", err)
		}
		to, err := s.Store.LedgerOf(ctx, tx, r.TrustAccountID, r.ToMatterID)
		if err != nil {
			return fmt.Errorf("destination ledger: %w", err)
		}
		if _, err := s.Store.LockLedgers(ctx, tx, from, to); err != nil {
			return err
		}
		// rule: ARC-I-025
		// By-Law 9: a transfer never overdraws the source ledger; checked again at authorization.
		avail, err := s.Store.Available(ctx, tx, from, "")
		if err != nil {
			return err
		}
		if r.AmountCents > avail {
			return fmt.Errorf("available %d, requested %d: %w", avail, r.AmountCents, ErrTrustOverdraw)
		}
		id, err := s.Store.InsertTransfer(ctx, tx, from, to, r)
		if err != nil {
			return err
		}
		cents := r.AmountCents
		payload, err := json.Marshal(TransferPayload{TransferID: id, FromMatterID: r.FromMatterID,
			ToMatterID: r.ToMatterID, AmountCents: &cents, Reason: r.Reason})
		if err != nil {
			return fmt.Errorf("payload: %w", err)
		}
		subject := r.Subject
		if subject.Kind == "" {
			subject = gate.Subject{Kind: SubjectTransfer, ID: id}
		}
		// rule: ARC-I-019
		// The preparer is the gate's requester, so they never decide it.
		gid, err := s.Gates.Open(ctx, tx, TransferGateKind, subject, payload, r.Preparer)
		if err != nil {
			return fmt.Errorf("open transfer gate: %w", err)
		}
		if err := s.Store.RecordGate(ctx, tx, id, string(gid)); err != nil {
			return err
		}
		out = TransferRequested{ID: id, GateID: string(gid), FromLedger: from, ToLedger: to}
		return nil
	})
	if err != nil {
		return TransferRequested{}, fmt.Errorf("request transfer: %w", err)
	}
	return out, nil
}

// LedgerOf implements TransferRequestStore.
func (PGTransferStore) LedgerOf(ctx context.Context, tx pgx.Tx, account, matter string) (trust.LedgerID, error) {
	return ledgerOf(ctx, tx, account, matter)
}

// InsertTransfer implements TransferRequestStore.
func (PGTransferStore) InsertTransfer(ctx context.Context, tx pgx.Tx, from, to trust.LedgerID, r TransferRequest) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO trust.trust_transfer
  (from_ledger_id, to_ledger_id, amount_cents, status, requested_by, reason)
VALUES ($1::uuid, $2::uuid, $3, 'pending_authorization', $4::uuid, $5)
RETURNING id::text`, string(from), string(to), r.AmountCents, r.PreparedBy, r.Reason).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert transfer: %w", err)
	}
	return id, nil
}

// RecordGate implements TransferRequestStore.
func (PGTransferStore) RecordGate(ctx context.Context, tx pgx.Tx, id, gateID string) error {
	tag, err := tx.Exec(ctx, `UPDATE trust.trust_transfer SET approval_id = $2::uuid
WHERE id = $1::uuid AND status = 'pending_authorization'`, id, gateID)
	if err != nil {
		return fmt.Errorf("record transfer gate: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("record transfer gate: %w", ErrNotFound)
	}
	return nil
}

var _ TransferRequestStore = PGTransferStore{}
