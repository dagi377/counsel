package recon

import (
	"context"
	"fmt"
)

// LineStore keeps imported bank statement lines, keyed by (account, bank line id).
type LineStore interface {
	// Has reports whether the line is already imported for the account.
	Has(ctx context.Context, accountID, lineID string) (bool, error)
	// Add records one new line for the account.
	Add(ctx context.Context, accountID string, l StatementLine) error
}

// ImportResult says what one statement import did.
type ImportResult struct {
	Added   int
	Skipped int // lines already imported
	// Gap is true when the statement's opening balance differs from the previous statement's
	// closing balance: lines are missing between them. A gap is reported, never filled in.
	Gap bool
}

// ImportLines adds the statement's lines that are not already in the store. prev is the
// account's previous statement, or nil for the first one. A statement whose lines do not
// sum to its closing balance is refused and nothing is added. Nothing here writes to the
// trust book: differences become reconciling work, never an automatic correction.
func ImportLines(ctx context.Context, store LineStore, s Statement, prev *Statement) (ImportResult, error) {
	var res ImportResult
	if s.AccountID == "" {
		return res, fmt.Errorf("import lines: account missing: %w", ErrStatementMismatch)
	}
	// rule: ARC-U-032
	sum := s.OpeningCents
	for _, l := range s.Lines {
		if l.ID == "" {
			return res, fmt.Errorf("import lines: line without bank id: %w", ErrStatementMismatch)
		}
		sum += l.AmountCents
	}
	if sum != s.ClosingCents {
		return res, fmt.Errorf("import lines: opening plus lines %d, closing %d: %w", sum, s.ClosingCents, ErrStatementMismatch)
	}
	// rule: ARC-U-032
	res.Gap = prev != nil && prev.AccountID == s.AccountID && prev.ClosingCents != s.OpeningCents

	seen := map[string]bool{}
	for _, l := range s.Lines {
		if seen[l.ID] {
			res.Skipped++
			continue
		}
		seen[l.ID] = true
		ok, err := store.Has(ctx, s.AccountID, l.ID)
		if err != nil {
			return res, fmt.Errorf("import lines: lookup: %w", err)
		}
		if ok {
			res.Skipped++
			continue
		}
		if err := store.Add(ctx, s.AccountID, l); err != nil {
			return res, fmt.Errorf("import lines: add: %w", err)
		}
		res.Added++
	}
	return res, nil
}

// CheckJournal checks the journal side of the three-way reconciliation for one trust
// account: the trust bank control balance must equal the sum of its client ledgers.
func CheckJournal(controlCents int64, ledgerCents []int64) error {
	// rule: BIL-I-009
	var sum int64
	for _, c := range ledgerCents {
		sum += c
	}
	if sum != controlCents {
		return fmt.Errorf("check journal: control %d, ledgers %d: %w", controlCents, sum, ErrUnbalanced)
	}
	return nil
}
