package credit

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Page sizes PGReads uses when a filter's Limit is out of range.
const (
	defaultReadLimit = 50
	maxReadLimit     = 200
)

func readLimit(n int) int {
	switch {
	case n <= 0:
		return defaultReadLimit
	case n > maxReadLimit:
		return maxReadLimit
	}
	return n
}

// PGReads is Reads over billing.credit_note and billing.write_off_draft. A credit note is
// requested until its gate's effect issues it; a bad-debt credit note and every write-off
// draft read as kind write_off. Only an issued credit note has a number.
type PGReads struct {
	DB TxRunner
}

var _ Reads = PGReads{}

// entriesSQL is every credit note and write-off draft as one id-ordered stream. Database
// statuses map onto the openapi CreditNote status (requested, issued, rejected).
const entriesSQL = `SELECT id::text, coalesce(number, ''), bill_id::text, kind, amount_cents, tax_cents, reason,
  status, coalesce(gate_id::text, '')
FROM (
  SELECT id, number, bill_id,
    CASE WHEN kind = 'bad_debt_write_off' THEN 'write_off' ELSE 'credit' END AS kind,
    amount_cents, tax_cents, reason,
    CASE status WHEN 'rejected' THEN 'rejected'
                WHEN 'draft' THEN 'requested' WHEN 'pending_approval' THEN 'requested'
                ELSE 'issued' END AS status,
    approval_id AS gate_id
  FROM billing.credit_note
  UNION ALL
  SELECT id, NULL, bill_id, 'write_off', amount_cents, tax_cents, reason,
    CASE status WHEN 'approved' THEN 'issued' ELSE 'requested' END,
    credit_gate_id
  FROM billing.write_off_draft
) e`

const listEntriesSQL = entriesSQL + `
WHERE (NULLIF($1, '')::uuid IS NULL OR bill_id = NULLIF($1, '')::uuid)
  AND (NULLIF($2, '')::uuid IS NULL OR id > NULLIF($2, '')::uuid)
ORDER BY id LIMIT $3`

const oneEntrySQL = entriesSQL + ` WHERE id = $1::uuid`

func scanEntry(row pgx.Row) (Entry, error) {
	var e Entry
	if err := row.Scan(&e.ID, &e.Number, &e.BillID, &e.Kind, &e.AmountCents, &e.HSTCents, &e.ReasonCode,
		&e.Status, &e.GateID); err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
			return Entry{}, ErrNotFound
		}
		return Entry{}, fmt.Errorf("scan: %w", err)
	}
	return e, nil
}

// Entries implements Reads.
func (p PGReads) Entries(ctx context.Context, f ListFilter) ([]Entry, error) {
	var out []Entry
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, listEntriesSQL, f.BillID, f.After, readLimit(f.Limit))
		if err != nil {
			return fmt.Errorf("select: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanEntry(rows)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list credit entries: %w", err)
	}
	return out, nil
}

// Entry implements Reads. An unknown, hidden or malformed id is ErrNotFound.
func (p PGReads) Entry(ctx context.Context, id string) (Entry, error) {
	var out Entry
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = scanEntry(tx.QueryRow(ctx, oneEntrySQL, id))
		return err
	})
	if err != nil {
		return Entry{}, fmt.Errorf("credit entry %s: %w", id, err)
	}
	return out, nil
}
