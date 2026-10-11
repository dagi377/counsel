package trust

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/ledger"
)

// ErrJournalMismatch means the trust journal disagrees with its client ledgers (BIL-I-009).
var ErrJournalMismatch = errors.New("trust: journal and client ledgers disagree")

// GLSourceKind is the journal source kind of every trust book entry; the source id is the
// trust transaction id, so a replayed posting returns the first entry.
const GLSourceKind = "trust_transaction"

// Subject kinds on trust book lines: TR-1500 lines carry the trust account, TR-2500 lines the
// client ledger, so the per-account split of BIL-I-009 survives in the journal.
const (
	SubjectTrustAccount = "trust.trust_account"
	SubjectClientLedger = "trust.client_ledger"
)

// SignatoryGuard refuses a posting on an account without an active signatory. AccountStore
// implements it.
type SignatoryGuard interface {
	RequireSignatory(ctx context.Context, tx pgx.Tx, id AccountID) error
}

// ReceiveCheck is an extra check a feature file adds to Receive (for example the cash limit).
// It runs in the posting tx after the ledger lock and before the insert.
type ReceiveCheck func(ctx context.Context, s *LedgerStore, tx pgx.Tx, l Ledger, r Receipt) error

// receiveChecks holds the checks added through addReceiveCheck. Call it only from init().
var receiveChecks []ReceiveCheck

func addReceiveCheck(c ReceiveCheck) { receiveChecks = append(receiveChecks, c) }

// ledgerSums is a ledger's posted and cleared totals and its authorised-not-executed holds.
type ledgerSums struct {
	PostedCents  int64
	ClearedCents int64
	HeldCents    int64
}

// ledgerRepo is the storage LedgerStore posts through. pgLedgerRepo is the Postgres one; unit
// tests use an in-memory one.
type ledgerRepo interface {
	insertLedger(ctx context.Context, tx pgx.Tx, l Ledger) (LedgerID, error)
	// lockLedger reads the ledger row FOR UPDATE.
	lockLedger(ctx context.Context, tx pgx.Tx, id LedgerID) (Ledger, error)
	// getLedger reads the ledger row without a lock.
	getLedger(ctx context.Context, tx pgx.Tx, id LedgerID) (Ledger, error)
	// sums totals the ledger as of today; holds leave out exceptWithdrawal.
	sums(ctx context.Context, tx pgx.Tx, id LedgerID, today time.Time, exceptWithdrawal string) (ledgerSums, error)
	insertTxn(ctx context.Context, tx pgx.Tx, t Txn) (TxnID, error)
	txns(ctx context.Context, tx pgx.Tx, id LedgerID) ([]Txn, error)
	// accountTotals returns the account's journal total and the sum of its ledger balances.
	accountTotals(ctx context.Context, tx pgx.Tx, id AccountID) (journal, ledgers int64, err error)
}

// LedgerStore keeps client ledgers and posts trust transactions, mirrored to the trust book
// (TR-1500 / TR-2500) through the GL. Writes run in the caller's tx so a posting, its journal
// entry and the gate effect that authorised it commit or roll back together.
type LedgerStore struct {
	db      db.DB
	repo    ledgerRepo
	signers SignatoryGuard
	gl      ledger.Ledger
	clk     clock.Clock
}

// NewLedgerStore returns a LedgerStore over Postgres. gl posts the trust book; clk gives the
// business date and time zone (America/Toronto).
func NewLedgerStore(pool db.DB, signers SignatoryGuard, gl ledger.Ledger, clk clock.Clock) *LedgerStore {
	return &LedgerStore{db: pool, repo: pgLedgerRepo{}, signers: signers, gl: gl, clk: clk}
}

// OpenLedger implements Ledgers: one ledger per matter on one account.
func (s *LedgerStore) OpenLedger(ctx context.Context, tx pgx.Tx, l Ledger) (LedgerID, error) {
	if l.AccountID == "" || l.MatterID == "" || l.ClientID == "" {
		return "", fmt.Errorf("open ledger: missing field: %w", ErrInvalid)
	}
	if l.ProtectedCents < 0 || l.MinimumBalanceCents < 0 {
		return "", fmt.Errorf("open ledger: negative hold: %w", ErrInvalid)
	}
	if l.ProtectedCents > 0 && l.ProtectedPurpose == "" {
		return "", fmt.Errorf("open ledger: protected purpose: %w", ErrInvalid)
	}
	id, err := s.repo.insertLedger(ctx, tx, l)
	if err != nil {
		return "", fmt.Errorf("open ledger: %w", err)
	}
	return id, nil
}

// validMethod reports whether m is a known method.
func validMethod(m Method) bool {
	switch m {
	case MethodCheque, MethodEFT, MethodWire, MethodCash, MethodBankDraft, MethodCard, MethodInternal:
		return true
	}
	return false
}

func validateReceipt(r Receipt) error {
	if r.LedgerID == "" || r.Counterparty == "" || r.Purpose == "" || r.PostedBy == "" || r.TxnDate.IsZero() {
		return fmt.Errorf("missing field: %w", ErrInvalid)
	}
	if r.AmountCents <= 0 {
		return fmt.Errorf("amount: %w", ErrInvalid)
	}
	if !validMethod(r.Method) {
		return fmt.Errorf("method: %w", ErrInvalid)
	}
	// Only a cheque or bank draft may start uncleared.
	if r.ClearedOn.IsZero() && r.Method != MethodCheque && r.Method != MethodBankDraft {
		return fmt.Errorf("cleared on: %w", ErrInvalid)
	}
	return nil
}

// Receive implements Ledgers. It locks the ledger, requires an active signatory on its account
// (MIG-035), posts the receipt and mirrors it to the trust book: Dr TR-1500 / Cr TR-2500.
func (s *LedgerStore) Receive(ctx context.Context, tx pgx.Tx, r Receipt) (TxnID, error) {
	if err := validateReceipt(r); err != nil {
		return "", fmt.Errorf("receive: %w", err)
	}
	l, err := s.repo.lockLedger(ctx, tx, r.LedgerID)
	if err != nil {
		return "", fmt.Errorf("receive: %w", err)
	}
	// rule: MIG-035
	if err := s.signers.RequireSignatory(ctx, tx, l.AccountID); err != nil {
		return "", fmt.Errorf("receive: %w", err)
	}
	for _, c := range receiveChecks {
		if err := c(ctx, s, tx, l, r); err != nil {
			return "", fmt.Errorf("receive: %w", err)
		}
	}
	t := Txn{
		AccountID: l.AccountID, LedgerID: l.ID, Kind: KindReceipt, TxnDate: r.TxnDate, Method: r.Method,
		InstrumentNo: r.InstrumentNo, Counterparty: r.Counterparty, AmountCents: r.AmountCents,
		Purpose: r.Purpose, CashExceptionCode: r.CashExceptionCode, ClearedOn: r.ClearedOn, PostedBy: r.PostedBy,
	}
	id, err := s.post(ctx, tx, t)
	if err != nil {
		return "", fmt.Errorf("receive: %w", err)
	}
	return id, nil
}

// Disburse implements Ledgers. It runs only inside the trust.withdrawal gate effect, so it
// needs the withdrawal and the approval that authorised it. It locks the ledger row, re-checks
// the available balance under that lock and posts the disbursement, mirrored as
// Dr TR-2500 / Cr TR-1500, or returns ErrInsufficientFunds and posts nothing.
func (s *LedgerStore) Disburse(ctx context.Context, tx pgx.Tx, d Disbursement) (TxnID, error) {
	if d.LedgerID == "" || d.WithdrawalID == "" || d.ApprovalID == "" || d.Counterparty == "" ||
		d.Purpose == "" || d.PostedBy == "" || d.TxnDate.IsZero() {
		return "", fmt.Errorf("disburse: missing field: %w", ErrInvalid)
	}
	if d.AmountCents <= 0 {
		return "", fmt.Errorf("disburse: amount: %w", ErrInvalid)
	}
	if d.Method == MethodCash {
		return "", fmt.Errorf("disburse: %w", ErrCashPayout)
	}
	if !validMethod(d.Method) {
		return "", fmt.Errorf("disburse: method: %w", ErrInvalid)
	}
	// rule: ARC-I-025
	// Lock first, then read: a concurrent posting on this ledger waits for this tx.
	l, err := s.repo.lockLedger(ctx, tx, d.LedgerID)
	if err != nil {
		return "", fmt.Errorf("disburse: %w", err)
	}
	sum, err := s.repo.sums(ctx, tx, l.ID, s.today(ctx), d.WithdrawalID)
	if err != nil {
		return "", fmt.Errorf("disburse: %w", err)
	}
	// rule: ARC-I-025
	// The withdrawal's own hold is released by this posting, so it is left out of the holds.
	avail := AvailableCents(sum.ClearedCents, l.ProtectedCents, l.MinimumBalanceCents, sum.HeldCents)
	if d.AmountCents > avail || sum.PostedCents-d.AmountCents < 0 {
		return "", fmt.Errorf("disburse: available %d, requested %d: %w", avail, d.AmountCents, ErrInsufficientFunds)
	}
	t := Txn{
		AccountID: l.AccountID, LedgerID: l.ID, Kind: KindDisbursement, TxnDate: d.TxnDate, Method: d.Method,
		Counterparty: d.Counterparty, AmountCents: -d.AmountCents, Purpose: d.Purpose,
		WithdrawalID: d.WithdrawalID, ApprovalID: d.ApprovalID, ClearedOn: d.TxnDate, PostedBy: d.PostedBy,
	}
	id, err := s.post(ctx, tx, t)
	if err != nil {
		return "", fmt.Errorf("disburse: %w", err)
	}
	return id, nil
}

// post inserts t and its trust book entry in tx. Money in debits TR-1500 and credits TR-2500;
// money out the reverse.
func (s *LedgerStore) post(ctx context.Context, tx pgx.Tx, t Txn) (TxnID, error) {
	id, err := s.repo.insertTxn(ctx, tx, t)
	if err != nil {
		return "", err
	}
	bank := ledger.Line{Account: books.AcctTrustBank, Subject: ledger.Subject{Kind: SubjectTrustAccount, ID: string(t.AccountID)}}
	client := ledger.Line{Account: books.AcctClientTrustLedger, Subject: ledger.Subject{Kind: SubjectClientLedger, ID: string(t.LedgerID)}}
	amt := t.AmountCents
	bank.Side, client.Side = ledger.Debit, ledger.Credit
	if amt < 0 {
		amt = -amt
		bank.Side, client.Side = ledger.Credit, ledger.Debit
	}
	bank.Amount, client.Amount = ledger.Cents(amt), ledger.Cents(amt)
	// rule: BIL-I-009
	// Both trust book lines move by the same amount in one entry, so TR-1500 per account always
	// equals the sum of TR-2500 over that account's ledgers.
	e := ledger.Entry{
		Book: books.BookTrust, Date: clock.DateOf(t.TxnDate, s.clk.Location()), SourceKind: GLSourceKind,
		SourceID: string(id), Memo: string(t.Kind), Lines: []ledger.Line{bank, client},
	}
	if _, err := s.gl.Post(ctx, tx, e); err != nil {
		return "", fmt.Errorf("trust book: %w", err)
	}
	return id, nil
}

// today is the business date as a midnight time in loc.
func (s *LedgerStore) today(ctx context.Context) time.Time {
	return s.clk.Today(ctx).Start(s.clk.Location())
}

// Balance implements Ledgers. Available counts authorised-not-executed withdrawals as holds
// (ARC-I-025).
func (s *LedgerStore) Balance(ctx context.Context, id LedgerID) (Balance, error) {
	var out Balance
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		l, err := s.repo.getLedger(ctx, tx, id)
		if err != nil {
			return err
		}
		sum, err := s.repo.sums(ctx, tx, id, s.today(ctx), "")
		if err != nil {
			return err
		}
		out = Balance{
			LedgerID: id, PostedCents: sum.PostedCents, ClearedCents: sum.ClearedCents,
			ProtectedCents: l.ProtectedCents, MinimumCents: l.MinimumBalanceCents,
			AvailableCents: AvailableCents(sum.ClearedCents, l.ProtectedCents, l.MinimumBalanceCents, sum.HeldCents),
		}
		return nil
	})
	if err != nil {
		return Balance{}, fmt.Errorf("balance %s: %w", id, err)
	}
	return out, nil
}

// Txns implements Ledgers.
func (s *LedgerStore) Txns(ctx context.Context, id LedgerID) ([]Txn, error) {
	var out []Txn
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := s.repo.getLedger(ctx, tx, id); err != nil {
			return err
		}
		var err error
		out, err = s.repo.txns(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("txns %s: %w", id, err)
	}
	return out, nil
}

// CheckJournal checks BIL-I-009 for one account inside tx: the account's journal total equals
// the sum of its client ledger balances. It returns ErrJournalMismatch, wrapped, when not.
func (s *LedgerStore) CheckJournal(ctx context.Context, tx pgx.Tx, id AccountID) error {
	journal, ledgers, err := s.repo.accountTotals(ctx, tx, id)
	if err != nil {
		return fmt.Errorf("check journal %s: %w", id, err)
	}
	// rule: BIL-I-009
	if journal != ledgers {
		return fmt.Errorf("check journal %s: journal %d, ledgers %d: %w", id, journal, ledgers, ErrJournalMismatch)
	}
	return nil
}

// pgLedgerRepo is ledgerRepo over trust.client_ledger and trust.trust_transaction.
type pgLedgerRepo struct{}

const insertLedgerSQL = `INSERT INTO trust.client_ledger
  (account_id, matter_id, client_id, protected_cents, protected_purpose, minimum_balance_cents)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, NULLIF($5, ''), $6)
RETURNING id::text`

func (pgLedgerRepo) insertLedger(ctx context.Context, tx pgx.Tx, l Ledger) (LedgerID, error) {
	var id string
	if err := tx.QueryRow(ctx, insertLedgerSQL, string(l.AccountID), l.MatterID, l.ClientID,
		l.ProtectedCents, l.ProtectedPurpose, l.MinimumBalanceCents).Scan(&id); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == pgUniqueViolation {
			return "", ErrDuplicateLedger
		}
		return "", fmt.Errorf("insert: %w", err)
	}
	return LedgerID(id), nil
}

const selectLedgerSQL = `SELECT id::text, account_id::text, matter_id::text, client_id::text,
  protected_cents, coalesce(protected_purpose, ''), minimum_balance_cents
FROM trust.client_ledger WHERE id = $1::uuid`

// lockLedgerSQL is the per-ledger lock: postings on one ledger serialise on it.
const lockLedgerSQL = selectLedgerSQL + ` FOR UPDATE`

func scanLedger(row pgx.Row) (Ledger, error) {
	var l Ledger
	var id, acct string
	if err := row.Scan(&id, &acct, &l.MatterID, &l.ClientID, &l.ProtectedCents, &l.ProtectedPurpose,
		&l.MinimumBalanceCents); err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == pgInvalidText) {
			return Ledger{}, ErrNotFound
		}
		return Ledger{}, fmt.Errorf("select ledger: %w", err)
	}
	l.ID, l.AccountID = LedgerID(id), AccountID(acct)
	return l, nil
}

func (pgLedgerRepo) lockLedger(ctx context.Context, tx pgx.Tx, id LedgerID) (Ledger, error) {
	return scanLedger(tx.QueryRow(ctx, lockLedgerSQL, string(id)))
}

func (pgLedgerRepo) getLedger(ctx context.Context, tx pgx.Tx, id LedgerID) (Ledger, error) {
	return scanLedger(tx.QueryRow(ctx, selectLedgerSQL, string(id)))
}

// sumsSQL mirrors trust.available_cents: money out, non-receipts and receipts cleared by today
// (on the row or by a later clearance) count as cleared.
const sumsSQL = `SELECT
  coalesce(sum(t.amount_cents), 0)::bigint,
  coalesce(sum(t.amount_cents) FILTER (WHERE t.amount_cents < 0 OR t.kind <> 'receipt' OR t.cleared_on <= $2::date
    OR EXISTS (SELECT 1 FROM trust.receipt_clearance c WHERE c.trust_transaction_id = t.id AND c.cleared_on <= $2::date)), 0)::bigint,
  coalesce((SELECT sum(w.amount_cents) FROM trust.withdrawal_request w
    WHERE w.ledger_id = $1::uuid AND w.status = 'authorized' AND w.id::text IS DISTINCT FROM NULLIF($3, '')), 0)::bigint
FROM trust.trust_transaction t WHERE t.ledger_id = $1::uuid`

func (pgLedgerRepo) sums(ctx context.Context, tx pgx.Tx, id LedgerID, today time.Time, except string) (ledgerSums, error) {
	var s ledgerSums
	if err := tx.QueryRow(ctx, sumsSQL, string(id), today.Format(time.DateOnly), except).
		Scan(&s.PostedCents, &s.ClearedCents, &s.HeldCents); err != nil {
		return ledgerSums{}, fmt.Errorf("sums: %w", err)
	}
	return s, nil
}

const insertTxnSQL = `INSERT INTO trust.trust_transaction
  (account_id, ledger_id, kind, txn_date, method, instrument_no, counterparty, amount_cents, purpose,
   withdrawal_id, approval_id, cash_exception_code, cleared_on, posted_by)
VALUES ($1::uuid, $2::uuid, $3, $4::date, $5, NULLIF($6, ''), $7, $8, $9,
   NULLIF($10, '')::uuid, NULLIF($11, '')::uuid, NULLIF($12, ''), NULLIF($13, '')::date, $14::uuid)
RETURNING id::text`

func dateOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.DateOnly)
}

func (pgLedgerRepo) insertTxn(ctx context.Context, tx pgx.Tx, t Txn) (TxnID, error) {
	var id string
	if err := tx.QueryRow(ctx, insertTxnSQL, string(t.AccountID), string(t.LedgerID), string(t.Kind),
		t.TxnDate.Format(time.DateOnly), string(t.Method), t.InstrumentNo, t.Counterparty, t.AmountCents, t.Purpose,
		t.WithdrawalID, t.ApprovalID, t.CashExceptionCode, dateOrEmpty(t.ClearedOn), t.PostedBy).Scan(&id); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23514" {
			// check_violation: the database's own balance guard (DM-052, DM-064).
			return "", fmt.Errorf("insert txn: %w", ErrInsufficientFunds)
		}
		return "", fmt.Errorf("insert txn: %w", err)
	}
	return TxnID(id), nil
}

const selectTxnsSQL = `SELECT id::text, account_id::text, ledger_id::text, kind, txn_date, method,
  coalesce(instrument_no, ''), counterparty, amount_cents, purpose, coalesce(reverses_id::text, ''),
  coalesce(replenishes_id::text, ''), coalesce(withdrawal_id::text, ''), coalesce(approval_id::text, ''),
  coalesce(cash_exception_code, ''), cleared_on, posted_by::text
FROM trust.trust_transaction WHERE ledger_id = $1::uuid ORDER BY posted_at, id`

func (pgLedgerRepo) txns(ctx context.Context, tx pgx.Tx, id LedgerID) ([]Txn, error) {
	rows, err := tx.Query(ctx, selectTxnsSQL, string(id))
	if err != nil {
		return nil, fmt.Errorf("txns: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Txn, error) {
		var t Txn
		var id, acct, led, kind, method, rev, rep string
		var cleared *time.Time
		if err := r.Scan(&id, &acct, &led, &kind, &t.TxnDate, &method, &t.InstrumentNo, &t.Counterparty,
			&t.AmountCents, &t.Purpose, &rev, &rep, &t.WithdrawalID, &t.ApprovalID, &t.CashExceptionCode,
			&cleared, &t.PostedBy); err != nil {
			return Txn{}, err
		}
		t.ID, t.AccountID, t.LedgerID = TxnID(id), AccountID(acct), LedgerID(led)
		t.Kind, t.Method, t.ReversesID, t.ReplenishesID = TxnKind(kind), Method(method), TxnID(rev), TxnID(rep)
		if cleared != nil {
			t.ClearedOn = *cleared
		}
		return t, nil
	})
	if err != nil {
		return nil, fmt.Errorf("txns: scan: %w", err)
	}
	return out, nil
}

const accountTotalsSQL = `SELECT
  (SELECT journal_balance_cents FROM trust.account_balance WHERE account_id = $1::uuid),
  (SELECT coalesce(sum(balance_cents), 0)::bigint FROM trust.ledger_balance WHERE account_id = $1::uuid)`

func (pgLedgerRepo) accountTotals(ctx context.Context, tx pgx.Tx, id AccountID) (int64, int64, error) {
	var journal *int64
	var ledgers int64
	if err := tx.QueryRow(ctx, accountTotalsSQL, string(id)).Scan(&journal, &ledgers); err != nil {
		return 0, 0, fmt.Errorf("account totals: %w", err)
	}
	if journal == nil {
		return 0, 0, ErrNotFound
	}
	return *journal, ledgers, nil
}
