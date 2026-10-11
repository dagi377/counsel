package recon

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PGImports writes a parsed bank statement (STMT-IMPORT): one trust.bank_import row with its
// opening and closing balances (migrations/5530_bank_import_balances.sql) and one
// trust.bank_line per transaction, in one transaction. It posts no journal entry: differences
// become reconciling work, never a correction. imported_by is the session user
// (core.current_user_id()), so an import outside a user session fails.
type PGImports struct {
	DB TxRunner
}

const sqlImportExists = `SELECT EXISTS (SELECT 1 FROM trust.bank_import
WHERE account_id = $1::uuid AND period && daterange($2::date, $3::date) AND closing_cents IS NOT NULL)`

const sqlInsertImport = `INSERT INTO trust.bank_import (account_id, file_ref, sha256, period, imported_by, opening_cents, closing_cents)
VALUES ($1::uuid, $2, $3, daterange($4::date, $5::date), core.current_user_id(), $6, $7)
RETURNING id::text`

// sqlInsertBankLine adds one line. A line the bank_line_dedupe index already holds (an
// overlapping re-import) is skipped, not duplicated.
const sqlInsertBankLine = `INSERT INTO trust.bank_line (account_kind, trust_account_id, import_id, posted_on, description, amount_cents, source_seq)
VALUES ('trust', $1::uuid, $2::uuid, $3::date, $4, $5, $6)
ON CONFLICT DO NOTHING`

// checkStatement refuses a statement that is not one whole, balanced month.
func checkStatement(s Statement) error {
	if !accountIDPattern.MatchString(s.AccountID) || s.Period.Year < 1 || s.Period.Month < time.January || s.Period.Month > time.December {
		return fmt.Errorf("import statement: account or month: %w", ErrUnparseable)
	}
	// rule: ARC-U-032
	sum := s.OpeningCents
	for _, l := range s.Lines {
		if l.AmountCents == 0 || l.Reference == "" {
			return fmt.Errorf("import statement: line without amount or description: %w", ErrUnparseable)
		}
		if l.Date.Year() != s.Period.Year || l.Date.Month() != s.Period.Month {
			return fmt.Errorf("import statement: line outside %04d-%02d: %w", s.Period.Year, int(s.Period.Month), ErrUnparseable)
		}
		sum += l.AmountCents
	}
	if sum != s.ClosingCents {
		return fmt.Errorf("import statement: opening plus lines %d, closing %d: %w", sum, s.ClosingCents, ErrStatementMismatch)
	}
	return nil
}

// statementDigest is the sha256 of the statement's account, month, balances and lines, so
// one statement imported twice is the same trust.bank_import row.
func statementDigest(s Statement) []byte {
	h := sha256.New()
	var b [8]byte
	put := func(n int64) {
		binary.BigEndian.PutUint64(b[:], uint64(n))
		h.Write(b[:])
	}
	h.Write([]byte(s.AccountID))
	put(int64(s.Period.Year))
	put(int64(s.Period.Month))
	put(s.OpeningCents)
	put(s.ClosingCents)
	for _, l := range s.Lines {
		h.Write([]byte(l.Date.Format(time.DateOnly)))
		put(l.AmountCents)
		put(int64(len(l.Reference)))
		h.Write([]byte(l.Reference))
	}
	return h.Sum(nil)
}

// ImportStatement records s and returns the new trust.bank_import id. A month already
// imported for the account is ErrDuplicateStatement; a reconciled month is ErrPeriodLocked.
func (p PGImports) ImportStatement(ctx context.Context, s Statement) (string, error) {
	if p.DB == nil {
		return "", fmt.Errorf("import statement: no database: %w", ErrInvalidState)
	}
	if err := checkStatement(s); err != nil {
		return "", err
	}
	from := time.Date(s.Period.Year, s.Period.Month, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	digest := statementDigest(s)
	var id string
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// rule: BIL-I-025
		locked, err := NewPGSignOff(tx).Locked(ctx, s.AccountID, s.Period)
		if err != nil {
			return err
		}
		if locked {
			return ErrPeriodLocked
		}
		var exists bool
		if err := tx.QueryRow(ctx, sqlImportExists, s.AccountID, from, to).Scan(&exists); err != nil {
			return fmt.Errorf("existing import: %w", err)
		}
		if exists {
			return ErrDuplicateStatement
		}
		ref := "csv:" + hex.EncodeToString(digest)
		if err := tx.QueryRow(ctx, sqlInsertImport, s.AccountID, ref, digest, from, to, s.OpeningCents, s.ClosingCents).Scan(&id); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return ErrDuplicateStatement
			}
			return fmt.Errorf("insert import: %w", err)
		}
		// source_seq numbers identical (date, amount, description) lines within the statement.
		type key struct {
			date   string
			amount int64
			desc   string
		}
		seq := map[key]int{}
		for _, l := range s.Lines {
			k := key{l.Date.Format(time.DateOnly), l.AmountCents, l.Reference}
			seq[k]++
			if _, err := tx.Exec(ctx, sqlInsertBankLine, s.AccountID, id, l.Date, l.Reference, l.AmountCents, seq[k]); err != nil {
				return fmt.Errorf("insert bank line: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("import statement for %s %04d-%02d: %w", s.AccountID, s.Period.Year, int(s.Period.Month), err)
	}
	return id, nil
}
