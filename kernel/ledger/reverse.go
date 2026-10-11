package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
)

// sourceReversal is the journal_entry.source_type of every reversing entry.
const sourceReversal = "reversal"

// Statements Reverse issues. Reverse only reads and inserts.
// rule: BIL-I-003
const (
	sqlEntryExists  = `SELECT EXISTS (SELECT 1 FROM gl.journal_entry WHERE id = $1::uuid)`
	sqlReversalOf   = `SELECT id::text FROM gl.journal_entry WHERE reverses_id = $1::uuid`
	sqlOriginalRows = `SELECT account_id::text, debit_cents, credit_cents, party, tax_code, bank_line_id::text
FROM gl.journal_line WHERE entry_id = $1::uuid ORDER BY id`
	sqlInsertReversal = `INSERT INTO gl.journal_entry (entry_date, memo, source_type, source_id, reverses_id, posted_by)
VALUES ($1, $2, 'reversal', $3::uuid, $3::uuid, $4::uuid)
ON CONFLICT (reverses_id) DO NOTHING
RETURNING id::text`
	sqlInsertReversalLine = `INSERT INTO gl.journal_line (entry_id, account_id, debit_cents, credit_cents, party, tax_code, bank_line_id)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::uuid)`
)

// reverseStatements lists every statement Reverse may run, for the append-only check.
var reverseStatements = []string{sqlEntryExists, sqlReversalOf, sqlOriginalRows, sqlInsertReversal, sqlInsertReversalLine}

// lineRow is one stored journal_line as Reverse reads and writes it. Optional columns are
// nil when NULL so the mirror copies them unchanged.
type lineRow struct {
	AccountID  string
	Debit      int64
	Credit     int64
	Party      *string
	TaxCode    *string
	BankLineID *string
}

// mirrorRows returns rows with debit and credit swapped and every other column unchanged.
// rule: BIL-I-019
func mirrorRows(rows []lineRow) []lineRow {
	out := make([]lineRow, len(rows))
	for i, r := range rows {
		r.Debit, r.Credit = r.Credit, r.Debit
		out[i] = r
	}
	return out
}

// Mirror returns lines with sides swapped; accounts, amounts and subjects are unchanged.
// rule: BIL-I-019
func Mirror(lines []Line) []Line {
	out := make([]Line, len(lines))
	for i, l := range lines {
		switch l.Side {
		case Debit:
			l.Side = Credit
		case Credit:
			l.Side = Debit
		}
		out[i] = l
	}
	return out
}

// Reverse posts the mirror of id inside tx: the same lines with sides swapped, dated today
// in the business time zone. Today always lies in the current period, so a reversal of an
// entry in a closed period lands in the current one (BIL-I-025). The original rows are
// never touched. An entry reverses at most once; a second call returns ErrAlreadyReversed.
func (g *PG) Reverse(ctx context.Context, tx pgx.Tx, id EntryID, reason string) (EntryID, error) {
	if id == "" || reason == "" {
		return "", fmt.Errorf("reverse %s: id and reason required: %w", id, ErrInvalidEntry)
	}
	var exists bool
	if err := tx.QueryRow(ctx, sqlEntryExists, string(id)).Scan(&exists); err != nil {
		return "", fmt.Errorf("reverse %s: find entry: %w", id, err)
	}
	if !exists {
		return "", fmt.Errorf("reverse %s: %w", id, ErrNotFound)
	}
	var prior string
	err := tx.QueryRow(ctx, sqlReversalOf, string(id)).Scan(&prior)
	switch {
	case err == nil:
		return "", fmt.Errorf("reverse %s: %w", id, ErrAlreadyReversed)
	case !errors.Is(err, pgx.ErrNoRows):
		return "", fmt.Errorf("reverse %s: find reversal: %w", id, err)
	}

	rows, err := readLines(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("reverse %s: entry has no lines: %w", id, ErrInvalidEntry)
	}

	if g.PostedBy == nil {
		return "", fmt.Errorf("reverse %s: %w", id, ErrNoPoster)
	}
	by, err := g.PostedBy(ctx)
	if err != nil || by == "" {
		return "", fmt.Errorf("reverse %s: %w", id, errors.Join(ErrNoPoster, err))
	}

	today := clock.Today(ctx)
	date := time.Date(today.Year, today.Month, today.Day, 0, 0, 0, 0, time.UTC)
	var rid string
	err = tx.QueryRow(ctx, sqlInsertReversal, date, reason, string(id), by).Scan(&rid)
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent transaction reversed the same entry first.
		return "", fmt.Errorf("reverse %s: %w", id, ErrAlreadyReversed)
	}
	if err != nil {
		return "", fmt.Errorf("reverse %s: insert entry: %w", id, err)
	}
	for i, r := range mirrorRows(rows) {
		if _, err := tx.Exec(ctx, sqlInsertReversalLine, rid, r.AccountID, r.Debit, r.Credit, r.Party, r.TaxCode, r.BankLineID); err != nil {
			return "", fmt.Errorf("reverse %s: insert line %d: %w", id, i, err)
		}
	}
	return EntryID(rid), nil
}

func readLines(ctx context.Context, tx pgx.Tx, id EntryID) ([]lineRow, error) {
	q, err := tx.Query(ctx, sqlOriginalRows, string(id))
	if err != nil {
		return nil, fmt.Errorf("reverse %s: read lines: %w", id, err)
	}
	defer q.Close()
	var out []lineRow
	for q.Next() {
		var r lineRow
		if err := q.Scan(&r.AccountID, &r.Debit, &r.Credit, &r.Party, &r.TaxCode, &r.BankLineID); err != nil {
			return nil, fmt.Errorf("reverse %s: scan line: %w", id, err)
		}
		out = append(out, r)
	}
	if err := q.Err(); err != nil {
		return nil, fmt.Errorf("reverse %s: read lines: %w", id, err)
	}
	return out, nil
}
