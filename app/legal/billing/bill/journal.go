package bill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/ledger"
)

// SourceBill is the gl.journal_entry source_type of a bill's approval entry (BIL-J-003). The
// source id is the bill id, so an approval posts at most one entry.
const SourceBill = "bill"

// TaxLine is one element of billing.bill.tax_breakdown: the tax one component charged on
// the bill. Amounts are int64 cents.
type TaxLine struct {
	TaxCode   string `json:"tax_code"`
	Component string `json:"component"` // HST, GST, PST, QST or NONE
	RatePPM   int64  `json:"rate_ppm"`
	BaseCents int64  `json:"base_cents"`
	TaxCents  int64  `json:"tax_cents"`
}

// JournalSource reads what the approval entry is built from, inside the gate's tx.
type JournalSource interface {
	// JournalLines returns the bill's lines in ordinal order and its tax breakdown.
	JournalLines(ctx context.Context, tx pgx.Tx, id ID) ([]Line, []TaxLine, error)
}

// Poster posts a balanced journal entry inside tx; ledger.Ledger satisfies it.
type Poster interface {
	Post(ctx context.Context, tx pgx.Tx, e ledger.Entry) (ledger.EntryID, error)
}

// TaxPayableAccount is the operating account a tax component is payable to: GST and HST
// to OP-2100, PST and QST to OP-2110.
func TaxPayableAccount(component string) (string, error) {
	switch component {
	case "HST", "GST":
		return books.AcctSalesTaxPayable, nil
	case "PST", "QST":
		return books.AcctProvincialTax, nil
	}
	return "", fmt.Errorf("tax component %q: %w", component, ErrInvalid)
}

// feeLine reports whether a bill line is a fee (time, flat fee or fee adjustment) rather than
// a disbursement.
func feeLine(s SourceType) (bool, error) {
	switch s {
	case SourceTime, SourceFlat, SourceAdjustment:
		return true, nil
	case SourceExpense:
		return false, nil
	}
	return false, fmt.Errorf("line source %q: %w", s, ErrInvalid)
}

// ApprovalJournal builds the accrual entry the approval of b posts in the operating book,
// dated the bill date (BIL-J-003). It has no I/O:
//
//	Dr OP-1100 AR                    total
//	Dr OP-4900 fee adjustments       discount, and each negative fee adjustment line
//	Cr OP-4000 fee revenue           each fee line
//	Cr OP-4010 cost recovery         taxable (own-cost) disbursements
//	Cr OP-1210 disb. recoverable     non-taxable (agent) disbursements, at cost
//	Cr OP-2100 / OP-2110             each tax line, by component
//
// The lines and tax lines must add up to the bill's frozen totals; any difference is
// ErrInvalid, so a bill never posts an entry that disagrees with what the client is billed.
func ApprovalJournal(b Bill, taxes []TaxLine) (ledger.Entry, error) {
	if b.ID == "" || b.MatterID == "" || b.IssueDate.IsZero() {
		return ledger.Entry{}, fmt.Errorf("approval journal %s: %w", b.ID, ErrInvalid)
	}
	if b.TotalCents != b.FeesCents+b.TaxableDisbCents+b.NontaxableDisbCents-b.DiscountCents+b.TaxCents {
		return ledger.Entry{}, fmt.Errorf("approval journal %s: total does not add up: %w", b.ID, ErrInvalid)
	}
	// rule: BIL-J-003
	dims := map[string]string{"matter_id": b.MatterID}
	line := func(acct string, side ledger.Side, amt int64) ledger.Line {
		return ledger.Line{Account: acct, Side: side, Amount: ledger.Cents(amt), Dimensions: dims}
	}
	e := ledger.Entry{Book: books.BookOperating, Date: clock.DateOf(b.IssueDate, business),
		SourceKind: SourceBill, SourceID: string(b.ID), Memo: "bill " + b.Number + " approved"}
	if b.TotalCents > 0 {
		e.Lines = append(e.Lines, line(books.AcctARFees, ledger.Debit, b.TotalCents))
	}
	if b.DiscountCents > 0 {
		e.Lines = append(e.Lines, line(books.AcctFeeAdjustments, ledger.Debit, b.DiscountCents))
	}
	var fees, disb int64
	for _, l := range b.Lines {
		fee, err := feeLine(l.Source)
		if err != nil {
			return ledger.Entry{}, fmt.Errorf("approval journal %s line %d: %w", b.ID, l.Ordinal, err)
		}
		if !fee {
			disb += l.AmountCents
			continue
		}
		fees += l.AmountCents
		switch {
		case l.AmountCents > 0:
			e.Lines = append(e.Lines, line(books.AcctFeeRevenue, ledger.Credit, l.AmountCents))
		case l.AmountCents < 0:
			e.Lines = append(e.Lines, line(books.AcctFeeAdjustments, ledger.Debit, -l.AmountCents))
		}
	}
	if fees != b.FeesCents || disb != b.TaxableDisbCents+b.NontaxableDisbCents {
		return ledger.Entry{}, fmt.Errorf("approval journal %s: lines (fees %d, disbursements %d) differ from the bill: %w",
			b.ID, fees, disb, ErrInvalid)
	}
	if b.TaxableDisbCents > 0 {
		e.Lines = append(e.Lines, line(books.AcctCostRecoveryRevenue, ledger.Credit, b.TaxableDisbCents))
	}
	if b.NontaxableDisbCents > 0 {
		e.Lines = append(e.Lines, line(books.AcctDisbRecoverable, ledger.Credit, b.NontaxableDisbCents))
	}
	var tax int64
	for i, t := range taxes {
		if t.TaxCents < 0 {
			return ledger.Entry{}, fmt.Errorf("approval journal %s tax line %d: %w", b.ID, i, ErrInvalid)
		}
		tax += t.TaxCents
		if t.TaxCents == 0 {
			continue // zero-rated or exempt: nothing payable
		}
		acct, err := TaxPayableAccount(t.Component)
		if err != nil {
			return ledger.Entry{}, fmt.Errorf("approval journal %s tax line %d: %w", b.ID, i, err)
		}
		tl := line(acct, ledger.Credit, t.TaxCents)
		tl.Dimensions = map[string]string{"matter_id": b.MatterID, "tax_code": t.TaxCode}
		e.Lines = append(e.Lines, tl)
	}
	if tax != b.TaxCents {
		return ledger.Entry{}, fmt.Errorf("approval journal %s: tax lines %d differ from bill tax %d: %w", b.ID, tax, b.TaxCents, ErrInvalid)
	}
	if err := ledger.Validate(e); err != nil {
		return ledger.Entry{}, fmt.Errorf("approval journal %s: %w", b.ID, err)
	}
	return e, nil
}

var (
	_ JournalSource   = PGStore{}
	_ ApprovalEntries = PGStore{}
)

// JournalLines reads the bill's lines and its tax breakdown.
func (PGStore) JournalLines(ctx context.Context, tx pgx.Tx, id ID) ([]Line, []TaxLine, error) {
	rows, err := tx.Query(ctx, `
SELECT ordinal, source_type, coalesce(time_entry_id::text, expense_id::text, ''), narrative, quantity_milli,
       rate_cents, amount_cents, tax_code
  FROM billing.bill_line WHERE bill_id = $1::uuid ORDER BY ordinal`, string(id))
	if err != nil {
		return nil, nil, fmt.Errorf("lines of bill %s: %w", id, err)
	}
	defer rows.Close()
	var lines []Line
	for rows.Next() {
		var l Line
		var src string
		if err := rows.Scan(&l.Ordinal, &src, &l.SourceID, &l.Narrative, &l.QuantityMilli, &l.RateCents, &l.AmountCents, &l.TaxCode); err != nil {
			return nil, nil, fmt.Errorf("lines of bill %s: %w", id, err)
		}
		l.Source = SourceType(src)
		lines = append(lines, l)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("lines of bill %s: %w", id, err)
	}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT tax_breakdown FROM billing.bill WHERE id = $1::uuid`, string(id)).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, fmt.Errorf("tax of bill %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("tax of bill %s: %w", id, err)
	}
	var taxes []TaxLine
	if err := json.Unmarshal(raw, &taxes); err != nil {
		return nil, nil, fmt.Errorf("tax of bill %s: %w", id, ErrInvalid)
	}
	return lines, taxes, nil
}

// ApprovalEntry finds the entry the approval of bill id posted: the one entry with source
// (bill, id) that is not itself a reversal. No entry is ledger.ErrNotFound.
func (PGStore) ApprovalEntry(ctx context.Context, tx pgx.Tx, id ID) (ledger.EntryID, error) {
	var e string
	err := tx.QueryRow(ctx, `SELECT id::text FROM gl.journal_entry
 WHERE source_type = $1 AND source_id = $2::uuid AND reverses_id IS NULL`, SourceBill, string(id)).Scan(&e)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("approval entry of bill %s: %w", id, ledger.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("approval entry of bill %s: %w", id, err)
	}
	return ledger.EntryID(e), nil
}
