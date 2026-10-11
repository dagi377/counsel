package ledger

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
)

// sourceReversal is the journal_entry.source_type of every reversing entry.
const sourceReversal = "reversal"

// Statements Reverse issues. Reverse only reads and inserts.
// rule: BIL-I-003
const (
	sqlEntryExists = `SELECT EXISTS (SELECT 1 FROM gl.journal_entry WHERE id = $1::uuid)`
	sqlReversalOf  = `SELECT id::text FROM gl.journal_entry WHERE reverses_id = $1::uuid`
	// The reversal stays in the original entry's book.
	sqlInsertReversal = `INSERT INTO gl.journal_entry (entry_date, memo, source_type, source_id, reverses_id, posted_by, book)
SELECT $1, $2, 'reversal', $3::uuid, $3::uuid, $4::uuid, o.book FROM gl.journal_entry o WHERE o.id = $3::uuid
ON CONFLICT (reverses_id) DO NOTHING
RETURNING id::text`
)

// reverseStatements returns every statement Reverse may run over dims, for the
// append-only check.
func reverseStatements(dims []dimension) []string {
	return []string{sqlEntryExists, sqlReversalOf, sqlLineDimensions, selectLinesSQL(dims), sqlInsertReversal, insertLineSQL(dims)}
}

// lineRow is one stored journal_line as Reverse reads and writes it. Dims holds every
// optional column (the dimensions) by name, as text; a NULL column is absent, so the
// mirror copies each one unchanged whatever app column it is.
type lineRow struct {
	AccountID string
	Debit     int64
	Credit    int64
	Dims      map[string]*string
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

// Mirror returns lines with sides swapped; accounts, amounts, subjects and dimensions are
// unchanged (each mirror line gets its own copy of the dimension map).
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
		l.Dimensions = maps.Clone(l.Dimensions)
		out[i] = l
	}
	return out
}

// Reverse posts the mirror of id inside tx: the same lines with sides swapped and every line
// dimension (each optional journal_line column, app links included) copied, dated today
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

	dims, err := lineDimensions(ctx, tx)
	if err != nil {
		return "", fmt.Errorf("reverse %s: %w", id, err)
	}
	rows, err := readLines(ctx, tx, id, dims)
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
	insertLine := insertLineSQL(dims)
	for i, r := range mirrorRows(rows) {
		args := append([]any{rid, r.AccountID, r.Debit, r.Credit}, dimArgs(r.Dims, dims)...)
		if _, err := tx.Exec(ctx, insertLine, args...); err != nil {
			return "", fmt.Errorf("reverse %s: insert line %d: %w", id, i, err)
		}
	}
	return EntryID(rid), nil
}

func readLines(ctx context.Context, tx pgx.Tx, id EntryID, dims []dimension) ([]lineRow, error) {
	q, err := tx.Query(ctx, selectLinesSQL(dims), string(id))
	if err != nil {
		return nil, fmt.Errorf("reverse %s: read lines: %w", id, err)
	}
	defer q.Close()
	var out []lineRow
	for q.Next() {
		r := lineRow{Dims: map[string]*string{}}
		vals := make([]*string, len(dims))
		dest := []any{&r.AccountID, &r.Debit, &r.Credit}
		for i := range vals {
			dest = append(dest, &vals[i])
		}
		if err := q.Scan(dest...); err != nil {
			return nil, fmt.Errorf("reverse %s: scan line: %w", id, err)
		}
		for i, d := range dims {
			if vals[i] != nil {
				r.Dims[d.Name] = vals[i]
			}
		}
		out = append(out, r)
	}
	if err := q.Err(); err != nil {
		return nil, fmt.Errorf("reverse %s: read lines: %w", id, err)
	}
	return out, nil
}
