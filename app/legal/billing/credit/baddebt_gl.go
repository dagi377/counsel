package credit

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/ledger"
)

// SourceWriteOff is the gl.journal_entry source kind of a write-off's bad-debt entry. The
// source id is the write-off id, so one write-off posts at most one entry.
const SourceWriteOff = "write_off"

// LedgerPoster posts a balanced journal entry inside tx; ledger.Ledger satisfies it.
type LedgerPoster interface {
	Post(ctx context.Context, tx pgx.Tx, e ledger.Entry) (ledger.EntryID, error)
}

// WriteOffReader reads a stored write-off in the caller's tx; WriteOffStore satisfies it.
type WriteOffReader interface {
	Lock(ctx context.Context, tx pgx.Tx, id string) (WriteOff, error)
}

var _ BadDebtPoster = GLBadDebt{}

// GLBadDebt posts an approved write-off to the operating (fees) book as one entry (BIL-J-009):
//
//	Dr OP-6100 bad debt expense   written-off amount less the tax adjustment
//	Dr OP-2100 GST/HST payable    the tax adjustment, when there is one
//	Cr OP-1100 AR                 written-off amount
//
// The entry's source is write_off/<id>, so posting the same write-off again returns the
// first entry and posts nothing.
type GLBadDebt struct {
	Ledger    LedgerPoster
	WriteOffs WriteOffReader
}

// PostBadDebt implements BadDebtPoster. The write-off must already be approved and stored
// (WriteOffEffect saves it before posting); its amount is read in tx.
func (g GLBadDebt) PostBadDebt(ctx context.Context, tx pgx.Tx, writeOffID string, adj BadDebtAdjustment) error {
	if g.Ledger == nil || g.WriteOffs == nil {
		return fmt.Errorf("post bad debt %s: no ledger or write-off store: %w", writeOffID, ErrInvalidState)
	}
	w, err := g.WriteOffs.Lock(ctx, tx, writeOffID)
	if err != nil {
		return fmt.Errorf("post bad debt %s: %w", writeOffID, err)
	}
	e, err := BadDebtEntry(w, adj)
	if err != nil {
		return fmt.Errorf("post bad debt %s: %w", writeOffID, err)
	}
	if _, err := g.Ledger.Post(ctx, tx, e); err != nil {
		return fmt.Errorf("post bad debt %s: %w", writeOffID, err)
	}
	return nil
}

// BadDebtEntry builds the journal entry of approved write-off w with tax adjustment adj. It
// has no I/O. The entry is always in the operating book: a write-off never touches trust.
func BadDebtEntry(w WriteOff, adj BadDebtAdjustment) (ledger.Entry, error) {
	switch {
	case w.ID == "" || w.Status != StatusApproved || adj.BillID != w.BillID || adj.On.IsZero():
		return ledger.Entry{}, fmt.Errorf("bad debt entry %s: %w", w.ID, ErrInvalidState)
	case w.AmountCents <= 0 || adj.TaxCents < 0 || adj.TaxCents > w.AmountCents:
		return ledger.Entry{}, fmt.Errorf("bad debt entry %s: %w", w.ID, ErrInvalidAmount)
	}
	// rule: BIL-C-205
	// The open tax on the written-off amount is relieved in the period of the write-off.
	e := ledger.Entry{
		Book: books.BookOperating, Date: clock.DateOf(adj.On, adj.On.Location()),
		SourceKind: SourceWriteOff, SourceID: w.ID, Memo: "write-off of bill " + w.BillID,
	}
	if pre := w.AmountCents - adj.TaxCents; pre > 0 {
		e.Lines = append(e.Lines, ledger.Line{Account: books.AcctBadDebt, Side: ledger.Debit, Amount: ledger.Cents(pre)})
	}
	if adj.TaxCents > 0 {
		e.Lines = append(e.Lines, ledger.Line{Account: books.AcctSalesTaxPayable, Side: ledger.Debit, Amount: ledger.Cents(adj.TaxCents)})
	}
	e.Lines = append(e.Lines, ledger.Line{Account: books.AcctARFees, Side: ledger.Credit, Amount: ledger.Cents(w.AmountCents)})
	return e, nil
}
