package withdraw

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/ledger"
)

// TransferGateKind is the gate that authorises a ledger-to-ledger transfer (G11t).
const TransferGateKind = "trust.transfer"

// ErrCrossAccount is returned for a transfer between ledgers on different trust accounts: money
// that leaves a bank account goes through a withdrawal (G11), not a ledger transfer.
var ErrCrossAccount = errors.New("withdraw: transfer crosses trust accounts")

// Transfer moves trust money from one client ledger to another on the same trust account.
type Transfer struct {
	ID          string // trust.trust_transfer row
	FromLedger  trust.LedgerID
	ToLedger    trust.LedgerID
	AmountCents int64
	Reason      string
	ApprovalID  string    // the approved trust.transfer gate
	TxnDate     time.Time // business date in America/Toronto
	PostedBy    string
}

// TransferStore keeps transfers and their trust lines. Every method runs in the caller's tx.
type TransferStore interface {
	// LockLedgers locks both client ledger rows FOR UPDATE in ascending id order, so two
	// transfers in opposite directions cannot deadlock, and returns them in that order.
	LockLedgers(ctx context.Context, tx pgx.Tx, a, b trust.LedgerID) ([]trust.Ledger, error)
	// Available is the ledger's available balance net of authorised holds, leaving out the
	// hold of exceptTransfer.
	Available(ctx context.Context, tx pgx.Tx, id trust.LedgerID, exceptTransfer string) (int64, error)
	// MarkPosted moves an authorised transfer to posted, or returns ErrNotAuthorized.
	MarkPosted(ctx context.Context, tx pgx.Tx, transferID string) error
	// InsertLeg inserts one trust line of the transfer.
	InsertLeg(ctx context.Context, tx pgx.Tx, transferID string, t trust.Txn) (trust.TxnID, error)
}

// Transfers posts client-to-client transfers: a transfer_out line on the source ledger and a
// transfer_in line on the target, equal and opposite, plus one balanced trust book entry.
type Transfers struct {
	Store TransferStore
	GL    ledger.Ledger
	Loc   *time.Location // America/Toronto
}

func validateTransfer(x Transfer) error {
	if x.ID == "" || x.FromLedger == "" || x.ToLedger == "" || x.ApprovalID == "" || x.Reason == "" ||
		x.PostedBy == "" || x.TxnDate.IsZero() || x.AmountCents <= 0 || x.FromLedger == x.ToLedger {
		return fmt.Errorf("transfer: %w", ErrInvalid)
	}
	return nil
}

// TransferLegs returns the two trust lines a transfer posts: -amount on the source ledger and
// +amount on the target. They sum to zero, so the trust account's balance is unchanged.
func TransferLegs(x Transfer, from, to trust.Ledger) ([2]trust.Txn, error) {
	if err := validateTransfer(x); err != nil {
		return [2]trust.Txn{}, err
	}
	if from.ID != x.FromLedger || to.ID != x.ToLedger {
		return [2]trust.Txn{}, fmt.Errorf("transfer: ledgers: %w", ErrInvalid)
	}
	if from.AccountID != to.AccountID {
		return [2]trust.Txn{}, ErrCrossAccount
	}
	leg := func(l, other trust.Ledger, kind trust.TxnKind, cents int64) trust.Txn {
		return trust.Txn{
			AccountID: l.AccountID, LedgerID: l.ID, Kind: kind, TxnDate: x.TxnDate, Method: trust.MethodInternal,
			Counterparty: "matter " + other.MatterID, AmountCents: cents, Purpose: x.Reason,
			ApprovalID: x.ApprovalID, ClearedOn: x.TxnDate, PostedBy: x.PostedBy,
		}
	}
	return [2]trust.Txn{
		leg(from, to, trust.KindTransferOut, -x.AmountCents),
		leg(to, from, trust.KindTransferIn, x.AmountCents),
	}, nil
}

// Post posts an authorised transfer in tx. It locks both ledgers, re-checks the source
// ledger's available balance under the lock, marks the transfer posted and inserts its two
// lines and trust book entry. On any error the caller rolls tx back and nothing is posted.
// It returns ErrUnverified while FlagBylaw9Verified is off.
func (t *Transfers) Post(ctx context.Context, tx pgx.Tx, x Transfer) ([2]trust.TxnID, error) {
	var ids [2]trust.TxnID
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return ids, ErrUnverified
	}
	if err := validateTransfer(x); err != nil {
		return ids, err
	}
	locked, err := t.Store.LockLedgers(ctx, tx, x.FromLedger, x.ToLedger)
	if err != nil {
		return ids, fmt.Errorf("transfer: %w", err)
	}
	from, ok1 := findLedger(locked, x.FromLedger)
	to, ok2 := findLedger(locked, x.ToLedger)
	if !ok1 || !ok2 {
		return ids, fmt.Errorf("transfer: ledger: %w", trust.ErrNotFound)
	}
	legs, err := TransferLegs(x, from, to)
	if err != nil {
		return ids, err
	}
	avail, err := t.Store.Available(ctx, tx, x.FromLedger, x.ID)
	if err != nil {
		return ids, fmt.Errorf("transfer: %w", err)
	}
	if x.AmountCents > avail {
		return ids, fmt.Errorf("transfer: available %d, requested %d: %w", avail, x.AmountCents, ErrTrustOverdraw)
	}
	if err := t.Store.MarkPosted(ctx, tx, x.ID); err != nil {
		return ids, fmt.Errorf("transfer: %w", err)
	}
	for i, l := range legs {
		if ids[i], err = t.Store.InsertLeg(ctx, tx, x.ID, l); err != nil {
			return [2]trust.TxnID{}, fmt.Errorf("transfer: leg: %w", err)
		}
	}
	// Both client ledgers sit under TR-2500: the source is debited and the target credited by
	// the same amount; TR-1500 (the bank) does not move.
	amt := ledger.Cents(x.AmountCents)
	e := ledger.Entry{
		Book: books.BookTrust, Date: clock.DateOf(x.TxnDate, t.Loc), SourceKind: trust.GLSourceKind, SourceID: x.ID,
		Memo: "transfer", Lines: []ledger.Line{
			{Account: books.AcctClientTrustLedger, Side: ledger.Debit, Amount: amt,
				Subject: ledger.Subject{Kind: trust.SubjectClientLedger, ID: string(x.FromLedger)}},
			{Account: books.AcctClientTrustLedger, Side: ledger.Credit, Amount: amt,
				Subject: ledger.Subject{Kind: trust.SubjectClientLedger, ID: string(x.ToLedger)}},
		},
	}
	if _, err := t.GL.Post(ctx, tx, e); err != nil {
		return [2]trust.TxnID{}, fmt.Errorf("transfer: trust book: %w", err)
	}
	return ids, nil
}

func findLedger(ls []trust.Ledger, id trust.LedgerID) (trust.Ledger, bool) {
	i := slices.IndexFunc(ls, func(l trust.Ledger) bool { return l.ID == id })
	if i < 0 {
		return trust.Ledger{}, false
	}
	return ls[i], true
}

// PGTransferStore is TransferStore over trust.client_ledger, trust.trust_transfer and
// trust.trust_transaction.
type PGTransferStore struct{}

// LockLedgers implements TransferStore. ORDER BY runs below the row lock, so rows are locked
// in id order.
func (PGTransferStore) LockLedgers(ctx context.Context, tx pgx.Tx, a, b trust.LedgerID) ([]trust.Ledger, error) {
	rows, err := tx.Query(ctx, `SELECT id::text, account_id::text, matter_id::text FROM trust.client_ledger
WHERE id IN ($1::uuid, $2::uuid) ORDER BY id FOR UPDATE`, string(a), string(b))
	if err != nil {
		return nil, fmt.Errorf("lock ledgers: %w", err)
	}
	var out []trust.Ledger
	for rows.Next() {
		var id, acct string
		var l trust.Ledger
		if err := rows.Scan(&id, &acct, &l.MatterID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("lock ledgers: %w", err)
		}
		l.ID, l.AccountID = trust.LedgerID(id), trust.AccountID(acct)
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lock ledgers: %w", err)
	}
	return out, nil
}

// Available implements TransferStore with trust.available_cents, the database's formula.
func (PGTransferStore) Available(ctx context.Context, tx pgx.Tx, id trust.LedgerID, exceptTransfer string) (int64, error) {
	var avail int64
	if err := tx.QueryRow(ctx, `SELECT trust.available_cents($1::uuid, NULL, NULLIF($2, '')::uuid)`,
		string(id), exceptTransfer).Scan(&avail); err != nil {
		return 0, fmt.Errorf("available: %w", err)
	}
	return avail, nil
}

// MarkPosted implements TransferStore.
func (PGTransferStore) MarkPosted(ctx context.Context, tx pgx.Tx, transferID string) error {
	tag, err := tx.Exec(ctx, `UPDATE trust.trust_transfer SET status = 'posted' WHERE id = $1::uuid AND status = 'authorized'`, transferID)
	if err != nil {
		return fmt.Errorf("mark transfer posted: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotAuthorized
	}
	return nil
}

// InsertLeg implements TransferStore. trust_txn_once_per_transfer_leg refuses a second line
// for the same transfer and ledger.
func (PGTransferStore) InsertLeg(ctx context.Context, tx pgx.Tx, transferID string, t trust.Txn) (trust.TxnID, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO trust.trust_transaction
  (account_id, ledger_id, kind, txn_date, method, counterparty, amount_cents, purpose, transfer_id, approval_id, cleared_on, posted_by)
VALUES ($1::uuid, $2::uuid, $3, $4::date, $5, $6, $7, $8, $9::uuid, $10::uuid, $4::date, $11::uuid)
RETURNING id::text`, string(t.AccountID), string(t.LedgerID), string(t.Kind), t.TxnDate.Format(time.DateOnly), string(t.Method),
		t.Counterparty, t.AmountCents, t.Purpose, transferID, t.ApprovalID, t.PostedBy).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert transfer leg: %w", err)
	}
	return trust.TxnID(id), nil
}
