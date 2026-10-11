package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoPoster means the PG ledger has no PostedBy configured, or it returned no user.
var ErrNoPoster = errors.New("ledger: no posting user")

// PG is the Postgres ledger over gl.account, gl.journal_entry and gl.journal_line.
type PG struct {
	// PostedBy returns the user id recorded as journal_entry.posted_by.
	PostedBy func(ctx context.Context) (string, error)
}

// Statements Post issues. Post only reads and inserts.
// rule: BIL-I-003
const (
	sqlFindBySource = `SELECT id::text FROM gl.journal_entry
WHERE source_type = $1 AND source_id = $2::uuid AND reverses_id IS NULL`
	sqlAccountID   = `SELECT id FROM gl.account WHERE code = $1 AND active`
	sqlInsertEntry = `INSERT INTO gl.journal_entry (entry_date, memo, source_type, source_id, posted_by)
VALUES ($1, $2, $3, $4::uuid, $5::uuid)
ON CONFLICT (source_type, source_id) WHERE reverses_id IS NULL AND source_id IS NOT NULL DO NOTHING
RETURNING id::text`
	sqlInsertLine = `INSERT INTO gl.journal_line (entry_id, account_id, debit_cents, credit_cents)
VALUES ($1::uuid, $2, $3, $4)`
)

// postStatements returns every statement Post may run over dims, for the append-only check.
func postStatements(dims []dimension) []string {
	return []string{sqlFindBySource, sqlPeriodClosed, sqlLineDimensions, sqlAccountID, sqlInsertEntry, sqlInsertLine, insertLineSQL(dims)}
}

// Validate checks e before it reaches the database: a book, a source, at least two lines,
// valid sides, positive amounts and debits equal to credits.
// rule: BIL-I-001
func Validate(e Entry) error {
	if e.Book == "" || e.SourceKind == "" || e.SourceID == "" || len(e.Lines) < 2 {
		return fmt.Errorf("entry %s/%s: %w", e.SourceKind, e.SourceID, ErrInvalidEntry)
	}
	var dr, cr Cents
	for i, l := range e.Lines {
		if l.Account == "" || l.Amount <= 0 {
			return fmt.Errorf("entry %s/%s line %d: %w", e.SourceKind, e.SourceID, i, ErrInvalidEntry)
		}
		switch l.Side {
		case Debit:
			dr += l.Amount
		case Credit:
			cr += l.Amount
		default:
			return fmt.Errorf("entry %s/%s line %d side: %w", e.SourceKind, e.SourceID, i, ErrInvalidEntry)
		}
		if dr < 0 || cr < 0 {
			return fmt.Errorf("entry %s/%s: total overflows: %w", e.SourceKind, e.SourceID, ErrInvalidEntry)
		}
	}
	if dr != cr {
		return fmt.Errorf("entry %s/%s: %w", e.SourceKind, e.SourceID, ErrUnbalanced)
	}
	return nil
}

// Post writes e inside tx and returns its id. It is idempotent per (SourceKind, SourceID):
// a second Post of the same source returns the first entry's id and writes nothing. The
// deferred trigger journal_balanced re-checks the balance at commit. An entry dated in a
// closed period is ErrPeriodClosed (CheckOpen), before anything is written.
func (g *PG) Post(ctx context.Context, tx pgx.Tx, e Entry) (EntryID, error) {
	if err := Validate(e); err != nil {
		return "", err
	}
	var id string
	err := tx.QueryRow(ctx, sqlFindBySource, e.SourceKind, e.SourceID).Scan(&id)
	switch {
	case err == nil:
		return EntryID(id), nil
	case !errors.Is(err, pgx.ErrNoRows):
		return "", fmt.Errorf("post %s/%s: find source: %w", e.SourceKind, e.SourceID, err)
	}
	// rule: BIL-I-025
	if err := g.CheckOpen(ctx, tx, e.Date); err != nil {
		return "", fmt.Errorf("post %s/%s: %w", e.SourceKind, e.SourceID, err)
	}
	if g.PostedBy == nil {
		return "", fmt.Errorf("post %s/%s: %w", e.SourceKind, e.SourceID, ErrNoPoster)
	}
	by, err := g.PostedBy(ctx)
	if err != nil || by == "" {
		return "", fmt.Errorf("post %s/%s: %w", e.SourceKind, e.SourceID, errors.Join(ErrNoPoster, err))
	}

	accounts := make(map[string]string, len(e.Lines))
	for _, l := range e.Lines {
		if _, ok := accounts[l.Account]; ok {
			continue
		}
		var aid string
		if err := tx.QueryRow(ctx, sqlAccountID, l.Account).Scan(&aid); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", fmt.Errorf("post %s/%s account %q: %w", e.SourceKind, e.SourceID, l.Account, ErrUnknownAccount)
			}
			return "", fmt.Errorf("post %s/%s: account lookup: %w", e.SourceKind, e.SourceID, err)
		}
		accounts[l.Account] = aid
	}

	var dims []dimension
	if hasDimensions(e.Lines) {
		if dims, err = lineDimensions(ctx, tx); err != nil {
			return "", fmt.Errorf("post %s/%s: %w", e.SourceKind, e.SourceID, err)
		}
		if err := checkDimensions(e.Lines, dims); err != nil {
			return "", fmt.Errorf("post %s/%s: %w", e.SourceKind, e.SourceID, err)
		}
	}

	date := time.Date(e.Date.Year, e.Date.Month, e.Date.Day, 0, 0, 0, 0, time.UTC)
	err = tx.QueryRow(ctx, sqlInsertEntry, date, e.Memo, e.SourceKind, e.SourceID, by).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent transaction posted the same source first and has committed.
		if err := tx.QueryRow(ctx, sqlFindBySource, e.SourceKind, e.SourceID).Scan(&id); err != nil {
			return "", fmt.Errorf("post %s/%s: reread source: %w", e.SourceKind, e.SourceID, err)
		}
		return EntryID(id), nil
	}
	if err != nil {
		return "", fmt.Errorf("post %s/%s: insert entry: %w", e.SourceKind, e.SourceID, err)
	}
	insertLine := sqlInsertLine
	if dims != nil {
		insertLine = insertLineSQL(dims)
	}
	for i, l := range e.Lines {
		var dr, cr Cents
		if l.Side == Debit {
			dr = l.Amount
		} else {
			cr = l.Amount
		}
		args := []any{id, accounts[l.Account], int64(dr), int64(cr)}
		if dims != nil {
			args = append(args, dimArgs(lineDims(l.Dimensions), dims)...)
		}
		if _, err := tx.Exec(ctx, insertLine, args...); err != nil {
			return "", fmt.Errorf("post %s/%s: insert line %d: %w", e.SourceKind, e.SourceID, i, err)
		}
	}
	return EntryID(id), nil
}

// hasDimensions reports whether any line carries a dimension value.
func hasDimensions(lines []Line) bool {
	for _, l := range lines {
		if len(l.Dimensions) > 0 {
			return true
		}
	}
	return false
}
