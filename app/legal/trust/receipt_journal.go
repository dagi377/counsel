package trust

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateReceiptClassification is the gate kind (spec/gates.json G6) that classifies money
// received into trust. Its effect, never a route, posts a receipt to the client ledger
// (app/legal.GateOwned trust_txn.posted=receipt, Q-HTTP-16).
const GateReceiptClassification = "trust.receipt_classification"

// SubjectReceipt is the gate subject kind of a trust receipts journal row.
const SubjectReceipt = "trust.receipt"

// Classifications a trust.receipt_classification payload carries.
const (
	ClassTrust   = "trust"
	ClassGeneral = "general"
)

// ReceiptStatus is the state of one trust receipts journal row.
type ReceiptStatus string

// Receipt journal states (migrations/5520_trust_receipt_journal.sql).
const (
	// ReceiptPending is recorded the day the money arrives, before classification.
	ReceiptPending ReceiptStatus = "received_pending_classification"
	// ReceiptPosted is classified as trust and posted to the client ledger.
	ReceiptPosted ReceiptStatus = "posted"
	// ReceiptGeneral is classified as not trust money; nothing is posted to trust.
	ReceiptGeneral ReceiptStatus = "classified_general"
)

// ErrReceiptState means a journal row or gate payload does not allow the change.
var ErrReceiptState = errors.New("trust: receipt journal state does not allow this")

// JournalReceipt is one row of the trust receipts journal.
type JournalReceipt struct {
	ID           string
	AccountID    AccountID
	MatterID     string
	LedgerID     LedgerID
	ReceivedOn   time.Time // business date, America/Toronto
	AmountCents  int64
	Method       Method
	Payer        string
	InstrumentNo string
	Purpose      string
	PaymentID    string
	Proposed     string // ClassTrust or ClassGeneral
	Status       ReceiptStatus
	GateID       string
	TxnID        TxnID // set once posted
	RecordedBy   string
}

// ReceiptJournal keeps the trust receipts journal. Every method runs in the caller's tx.
type ReceiptJournal interface {
	// Record inserts r in state ReceiptPending and returns its id.
	Record(ctx context.Context, tx pgx.Tx, r JournalReceipt) (string, error)
	// SetGate links the row to the classification gate opened for it.
	SetGate(ctx context.Context, tx pgx.Tx, id, gateID string) error
	// Lock reads row id FOR UPDATE, or returns ErrNotFound.
	Lock(ctx context.Context, tx pgx.Tx, id string) (JournalReceipt, error)
	// Classify moves a pending row to status; txn is set only for ReceiptPosted.
	Classify(ctx context.Context, tx pgx.Tx, id string, status ReceiptStatus, txn TxnID) error
}

// ReceiptPoster posts a receipt to its client ledger; LedgerStore implements it.
type ReceiptPoster interface {
	Receive(ctx context.Context, tx pgx.Tx, r Receipt) (TxnID, error)
}

// ClassificationPayload is the trust.receipt_classification gate payload (spec/gates.json).
type ClassificationPayload struct {
	ReceiptID      string `json:"receipt_id"`
	MatterID       string `json:"matter_id"`
	AmountCents    int64  `json:"amount_cents"`
	Classification string `json:"classification"`
}

// ReceiptClassificationEffect applies an approved trust.receipt_classification gate. A trust
// classification posts the journal row to its client ledger (Dr TR-1500 / Cr TR-2500); a
// general one posts nothing to trust. It fails closed: on any error nothing is applied.
type ReceiptClassificationEffect struct {
	Journal ReceiptJournal
	Ledgers ReceiptPoster
}

// Effects returns the trust.receipt_classification effect for gates.RegisterWired. Every
// error wraps gate.ErrEffectFailed.
func (e *ReceiptClassificationEffect) Effects() map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{
		GateReceiptClassification: func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
			if err := e.Apply(ctx, tx, g); err != nil {
				return fmt.Errorf("%w: %w", gate.ErrEffectFailed, err)
			}
			return nil
		},
	}
}

// Apply is the EffectFunc of trust.receipt_classification.
func (e *ReceiptClassificationEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	if e.Journal == nil || e.Ledgers == nil {
		return fmt.Errorf("receipt classification %s: not configured: %w", g.GateID, ErrReceiptState)
	}
	var p ClassificationPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.ReceiptID == "" || p.ReceiptID != g.Subject.ID ||
		(p.Classification != ClassTrust && p.Classification != ClassGeneral) {
		return fmt.Errorf("receipt classification %s: payload: %w", g.GateID, ErrReceiptState)
	}
	// rule: ARC-I-012
	// The engine already refuses these; the effect checks again so a posting never runs on a
	// decision by an agent or by the person who recorded the receipt.
	if len(g.Approvers) == 0 {
		return fmt.Errorf("receipt classification %s: no approver: %w", g.GateID, ErrReceiptState)
	}
	approver := g.Approvers[0]
	if approver.Kind != registry.PrincipalUser || approver.UserID == preparerOf(g.Preparer) {
		return fmt.Errorf("receipt classification %s: approver: %w", g.GateID, ErrReceiptState)
	}
	r, err := e.Journal.Lock(ctx, tx, p.ReceiptID)
	if err != nil {
		return fmt.Errorf("receipt classification %s: %w", g.GateID, err)
	}
	if r.MatterID != p.MatterID || r.AmountCents != p.AmountCents {
		return fmt.Errorf("receipt classification %s: payload does not match receipt %s: %w", g.GateID, r.ID, ErrReceiptState)
	}
	// A replay of an applied effect changes nothing.
	if r.Status != ReceiptPending {
		return nil
	}
	if p.Classification == ClassGeneral {
		if err := e.Journal.Classify(ctx, tx, r.ID, ReceiptGeneral, ""); err != nil {
			return fmt.Errorf("receipt classification %s: %w", g.GateID, err)
		}
		return nil
	}
	// rule: BIL-I-009
	// Only here does the receipt reach the client ledger; Receive locks the ledger, checks the
	// signatory and the cash limit, and mirrors the posting to the trust book.
	rc := Receipt{
		LedgerID: r.LedgerID, TxnDate: r.ReceivedOn, Method: r.Method, InstrumentNo: r.InstrumentNo,
		Counterparty: r.Payer, AmountCents: r.AmountCents, Purpose: r.Purpose, PostedBy: approver.UserID,
	}
	if r.Method != MethodCheque && r.Method != MethodBankDraft {
		rc.ClearedOn = r.ReceivedOn
	}
	txn, err := e.Ledgers.Receive(ctx, tx, rc)
	if err != nil {
		return fmt.Errorf("receipt classification %s: %w", g.GateID, err)
	}
	if err := e.Journal.Classify(ctx, tx, r.ID, ReceiptPosted, txn); err != nil {
		return fmt.Errorf("receipt classification %s: %w", g.GateID, err)
	}
	return nil
}

// preparerOf is the user who counts as preparer: an agent's acting-for user.
func preparerOf(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}

// PGReceiptJournal is ReceiptJournal over trust.receipt_journal.
type PGReceiptJournal struct{}

var _ ReceiptJournal = PGReceiptJournal{}

const recordReceiptSQL = `INSERT INTO trust.receipt_journal
  (account_id, matter_id, ledger_id, received_on, amount_cents, method, payer, instrument_no, purpose,
   payment_id, proposed, recorded_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $5, $6, $7, NULLIF($8, ''), $9, NULLIF($10, '')::uuid, $11, $12::uuid)
RETURNING id::text`

// Record implements ReceiptJournal.
func (PGReceiptJournal) Record(ctx context.Context, tx pgx.Tx, r JournalReceipt) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, recordReceiptSQL, string(r.AccountID), r.MatterID, string(r.LedgerID),
		r.ReceivedOn.Format(time.DateOnly), r.AmountCents, string(r.Method), r.Payer, r.InstrumentNo, r.Purpose,
		r.PaymentID, r.Proposed, r.RecordedBy).Scan(&id); err != nil {
		return "", fmt.Errorf("record receipt: %w", err)
	}
	return id, nil
}

// SetGate implements ReceiptJournal.
func (PGReceiptJournal) SetGate(ctx context.Context, tx pgx.Tx, id, gateID string) error {
	tag, err := tx.Exec(ctx, `UPDATE trust.receipt_journal SET gate_id = $2::uuid
WHERE id = $1::uuid AND status = 'received_pending_classification'`, id, gateID)
	if err != nil {
		return fmt.Errorf("receipt %s: set gate: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("receipt %s: set gate: %w", id, ErrReceiptState)
	}
	return nil
}

const lockReceiptSQL = `SELECT id::text, account_id::text, matter_id::text, ledger_id::text, received_on,
  amount_cents, method, payer, coalesce(instrument_no, ''), purpose, coalesce(payment_id::text, ''), proposed,
  status, coalesce(gate_id::text, ''), coalesce(trust_transaction_id::text, ''), recorded_by::text
FROM trust.receipt_journal WHERE id = $1::uuid FOR UPDATE`

// Lock implements ReceiptJournal.
func (PGReceiptJournal) Lock(ctx context.Context, tx pgx.Tx, id string) (JournalReceipt, error) {
	var r JournalReceipt
	var acct, led, method, status, txn string
	err := tx.QueryRow(ctx, lockReceiptSQL, id).Scan(&r.ID, &acct, &r.MatterID, &led, &r.ReceivedOn,
		&r.AmountCents, &method, &r.Payer, &r.InstrumentNo, &r.Purpose, &r.PaymentID, &r.Proposed,
		&status, &r.GateID, &txn, &r.RecordedBy)
	if err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == pgInvalidText) {
			return JournalReceipt{}, ErrNotFound
		}
		return JournalReceipt{}, fmt.Errorf("lock receipt %s: %w", id, err)
	}
	r.AccountID, r.LedgerID, r.Method, r.Status, r.TxnID = AccountID(acct), LedgerID(led), Method(method), ReceiptStatus(status), TxnID(txn)
	return r, nil
}

// Classify implements ReceiptJournal.
func (PGReceiptJournal) Classify(ctx context.Context, tx pgx.Tx, id string, status ReceiptStatus, txn TxnID) error {
	tag, err := tx.Exec(ctx, `UPDATE trust.receipt_journal
SET status = $2, trust_transaction_id = NULLIF($3, '')::uuid, classified_at = now()
WHERE id = $1::uuid AND status = 'received_pending_classification'`, id, string(status), string(txn))
	if err != nil {
		return fmt.Errorf("classify receipt %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("classify receipt %s: %w", id, ErrReceiptState)
	}
	return nil
}
