package trust

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/ledger"
)

// ExceptionNSF is the trust exception kind an NSF reversal opens (trust.exception.kind).
const ExceptionNSF = "nsf"

// GLSourceNSFReplenishment is the journal source kind of the operating leg of an NSF
// replenishment (the 'nsf_replenishment' value of the gl.journal_entry source_type CHECK). The
// source id is the replenishing trust receipt.
const GLSourceNSFReplenishment = "nsf_replenishment"

// Counterparty and purposes written on NSF postings. They name no client.
const (
	nsfFirmCounterparty   = "Firm general account"
	nsfReversalPurpose    = "NSF reversal"
	nsfReplenishPurpose   = "NSF replenishment from firm funds"
	nsfOperatingLegMemo   = "nsf replenishment: receivable from client"
	nsfReversibleExplains = "only a cheque, bank draft, EFT, wire or card receipt can be returned"
)

// trustException is one trust.exception row.
type trustException struct {
	Kind           string
	ReceiptID      TxnID
	LedgerID       LedgerID
	MatterID       string
	AmountCents    int64
	ShortfallCents int64
	OpenedBy       string
}

// nsfRepo is the storage ReverseNSF needs beyond ledgerRepo. pgLedgerRepo implements it; the
// repo behind a LedgerStore must implement it to reverse NSF receipts.
type nsfRepo interface {
	// getTxn reads one trust transaction, or returns ErrNotFound.
	getTxn(ctx context.Context, tx pgx.Tx, id TxnID) (Txn, error)
	// reversalOf returns the id of the reversal of id, or "" when it is not reversed.
	reversalOf(ctx context.Context, tx pgx.Tx, id TxnID) (TxnID, error)
	// openException inserts a trust exception and returns its id.
	openException(ctx context.Context, tx pgx.Tx, e trustException) (string, error)
}

func (s *LedgerStore) nsfRepo() (nsfRepo, error) {
	r, ok := s.repo.(nsfRepo)
	if !ok {
		return nil, fmt.Errorf("nsf: store cannot reverse receipts: %w", ErrInvalid)
	}
	return r, nil
}

// returnable reports whether a receipt by method m can come back unpaid. Cash and internal
// transfers cannot bounce.
func returnable(m Method) bool {
	switch m {
	case MethodCheque, MethodBankDraft, MethodEFT, MethodWire, MethodCard:
		return true
	}
	return false
}

// ReverseNSF implements Ledgers. In tx it locks the receipt's ledger, posts the reversal and,
// only when the ledger would go negative because the money was already paid out, a receipt
// from the firm general account for exactly the shortfall under approvalID (G6), with its
// operating leg Dr OP-1100 / Cr OP-1000. Every reversal opens a trust exception. Behind
// FlagBylaw9Verified; off, it fails closed with ErrUnverified and posts nothing.
func (s *LedgerStore) ReverseNSF(ctx context.Context, tx pgx.Tx, receipt TxnID, approvalID, by string) (NSFResult, error) {
	// rule: BIL-C-065 (verify)
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return NSFResult{}, fmt.Errorf("reverse nsf: %w", ErrUnverified)
	}
	if receipt == "" || by == "" {
		return NSFResult{}, fmt.Errorf("reverse nsf: missing field: %w", ErrInvalid)
	}
	repo, err := s.nsfRepo()
	if err != nil {
		return NSFResult{}, fmt.Errorf("reverse nsf: %w", err)
	}
	orig, err := repo.getTxn(ctx, tx, receipt)
	if err != nil {
		return NSFResult{}, fmt.Errorf("reverse nsf %s: %w", receipt, err)
	}
	// Lock the ledger before any check: a concurrent reversal or payout on it waits for tx.
	l, err := s.repo.lockLedger(ctx, tx, orig.LedgerID)
	if err != nil {
		return NSFResult{}, fmt.Errorf("reverse nsf %s: %w", receipt, err)
	}
	if orig.Kind != KindReceipt {
		return NSFResult{}, fmt.Errorf("reverse nsf %s: kind %s: %w", receipt, orig.Kind, ErrNotReceipt)
	}
	if !returnable(orig.Method) {
		return NSFResult{}, fmt.Errorf("reverse nsf %s: method %s: %s: %w", receipt, orig.Method, nsfReversibleExplains, ErrInvalid)
	}
	// rule: DM-055
	// A receipt is reversed at most once (reverses_id is UNIQUE).
	rev, err := repo.reversalOf(ctx, tx, orig.ID)
	if err != nil {
		return NSFResult{}, fmt.Errorf("reverse nsf %s: %w", receipt, err)
	}
	if rev != "" {
		return NSFResult{}, fmt.Errorf("reverse nsf %s: %w", receipt, ErrAlreadyReversed)
	}
	sum, err := s.repo.sums(ctx, tx, l.ID, s.today(ctx), "")
	if err != nil {
		return NSFResult{}, fmt.Errorf("reverse nsf %s: %w", receipt, err)
	}
	// rule: BIL-C-065 (verify)
	// Replenish only if the ledger would go negative, and for exactly the shortfall.
	var shortfall int64
	if after := sum.PostedCents - orig.AmountCents; after < 0 {
		shortfall = -after
	}
	// rule: BIL-C-065 (verify)
	// Firm money into trust for a shortfall needs the G6 approval; it is never posted unapproved.
	if shortfall > 0 && approvalID == "" {
		return NSFResult{}, fmt.Errorf("reverse nsf %s: replenishment needs an approval: %w", receipt, ErrInvalid)
	}
	today := s.today(ctx)
	out := NSFResult{ShortfallCents: shortfall}
	// rule: DM-055
	// The reversal negates exactly the receipt, on the same ledger.
	out.ReversalID, err = s.post(ctx, tx, Txn{
		AccountID: l.AccountID, LedgerID: l.ID, Kind: KindReversal, TxnDate: today, Method: orig.Method,
		InstrumentNo: orig.InstrumentNo, Counterparty: orig.Counterparty, AmountCents: -orig.AmountCents,
		Purpose: nsfReversalPurpose, ReversesID: orig.ID, ClearedOn: today, PostedBy: by,
	})
	if err != nil {
		return NSFResult{}, fmt.Errorf("reverse nsf %s: reversal: %w", receipt, err)
	}
	if shortfall > 0 {
		if out.ReplenishmentID, err = s.replenish(ctx, tx, l, orig.ID, shortfall, approvalID, by); err != nil {
			return NSFResult{}, fmt.Errorf("reverse nsf %s: %w", receipt, err)
		}
	}
	// rule: BIL-C-065 (verify)
	// Every reversal of a trust deposit opens a trust exception, in the same tx.
	if _, err := repo.openException(ctx, tx, trustException{
		Kind: ExceptionNSF, ReceiptID: orig.ID, LedgerID: l.ID, MatterID: l.MatterID,
		AmountCents: orig.AmountCents, ShortfallCents: shortfall, OpenedBy: by,
	}); err != nil {
		return NSFResult{}, fmt.Errorf("reverse nsf %s: open exception: %w", receipt, err)
	}
	out.ExceptionOpened = true
	return out, nil
}

// replenish posts the firm-funds receipt for shortfall into l (trust book Dr TR-1500 /
// Cr TR-2500) and its operating leg Dr OP-1100 receivable from client / Cr OP-1000.
func (s *LedgerStore) replenish(ctx context.Context, tx pgx.Tx, l Ledger, nsf TxnID, shortfall int64, approvalID, by string) (TxnID, error) {
	today := s.today(ctx)
	// rule: BIL-C-065 (verify)
	id, err := s.post(ctx, tx, Txn{
		AccountID: l.AccountID, LedgerID: l.ID, Kind: KindReceipt, TxnDate: today, Method: MethodInternal,
		Counterparty: nsfFirmCounterparty, AmountCents: shortfall, Purpose: nsfReplenishPurpose,
		ReplenishesID: nsf, ApprovalID: approvalID, ClearedOn: today, PostedBy: by,
	})
	if err != nil {
		return "", fmt.Errorf("replenishment: %w", err)
	}
	amt := ledger.Cents(shortfall)
	// rule: BIL-C-065 (verify)
	// The firm paid the client's shortfall: a receivable from the client, out of operating.
	e := ledger.Entry{
		Book: books.BookOperating, Date: clock.DateOf(today, s.clk.Location()), SourceKind: GLSourceNSFReplenishment,
		SourceID: string(id), Memo: nsfOperatingLegMemo, Lines: []ledger.Line{
			{Account: books.AcctARFees, Side: ledger.Debit, Amount: amt, Subject: ledger.Subject{Kind: matterkind.Subject, ID: l.MatterID}},
			{Account: books.AcctOperatingBank, Side: ledger.Credit, Amount: amt},
		},
	}
	if _, err := s.gl.Post(ctx, tx, e); err != nil {
		return "", fmt.Errorf("replenishment operating leg: %w", err)
	}
	return id, nil
}

const selectTxnSQL = `SELECT id::text, account_id::text, ledger_id::text, kind, txn_date, method,
  coalesce(instrument_no, ''), counterparty, amount_cents, purpose, coalesce(reverses_id::text, ''),
  coalesce(replenishes_id::text, ''), coalesce(withdrawal_id::text, ''), coalesce(approval_id::text, ''),
  coalesce(cash_exception_code, ''), cleared_on, posted_by::text
FROM trust.trust_transaction WHERE id = $1::uuid`

func (pgLedgerRepo) getTxn(ctx context.Context, tx pgx.Tx, id TxnID) (Txn, error) {
	var t Txn
	var tid, acct, led, kind, method, rev, rep string
	var cleared *time.Time
	err := tx.QueryRow(ctx, selectTxnSQL, string(id)).Scan(&tid, &acct, &led, &kind, &t.TxnDate, &method,
		&t.InstrumentNo, &t.Counterparty, &t.AmountCents, &t.Purpose, &rev, &rep, &t.WithdrawalID, &t.ApprovalID,
		&t.CashExceptionCode, &cleared, &t.PostedBy)
	if err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == pgInvalidText) {
			return Txn{}, ErrNotFound
		}
		return Txn{}, fmt.Errorf("select txn: %w", err)
	}
	t.ID, t.AccountID, t.LedgerID = TxnID(tid), AccountID(acct), LedgerID(led)
	t.Kind, t.Method, t.ReversesID, t.ReplenishesID = TxnKind(kind), Method(method), TxnID(rev), TxnID(rep)
	if cleared != nil {
		t.ClearedOn = *cleared
	}
	return t, nil
}

func (pgLedgerRepo) reversalOf(ctx context.Context, tx pgx.Tx, id TxnID) (TxnID, error) {
	var rev string
	err := tx.QueryRow(ctx, `SELECT id::text FROM trust.trust_transaction WHERE reverses_id = $1::uuid`, string(id)).Scan(&rev)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reversal of: %w", err)
	}
	return TxnID(rev), nil
}

const insertExceptionSQL = `INSERT INTO trust.exception
  (kind, receipt_id, ledger_id, matter_id, amount_cents, shortfall_cents, opened_by)
VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7::uuid)
RETURNING id::text`

func (pgLedgerRepo) openException(ctx context.Context, tx pgx.Tx, e trustException) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, insertExceptionSQL, e.Kind, string(e.ReceiptID), string(e.LedgerID), e.MatterID,
		e.AmountCents, e.ShortfallCents, e.OpenedBy).Scan(&id); err != nil {
		return "", fmt.Errorf("insert exception: %w", err)
	}
	return id, nil
}
