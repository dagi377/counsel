package credit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ WriteOffTotals = PGWriteOffTotals{}

// PGWriteOffTotals is the Postgres WriteOffTotals. It locks the bill row FOR UPDATE first,
// so two write-offs on one bill are decided one after the other, then sums approved
// write-offs (billing.write_off) and submitted write-offs still in billing.write_off_draft
// (status draft with a G-B5 gate). It reads only billing tables, never the kernel gate
// table. A rejected or withdrawn draft does not count. Anything
// it cannot count fails closed.
type PGWriteOffTotals struct{}

// lockBillSQL locks the bill a write-off reduces and reads its payer (the client).
const lockBillSQL = `SELECT payer_id::text FROM billing.bill WHERE id = $1::uuid FOR UPDATE`

// clientBillsSQL lists the client's bills, so write-offs on them can count.
const clientBillsSQL = `SELECT id::text FROM billing.bill WHERE payer_id = $1::uuid ORDER BY id`

// approvedWriteOffsSQL lists approved write-offs on the bill, and on the client's bills
// created at or after the window start.
const approvedWriteOffsSQL = `SELECT id::text, amount_cents
FROM billing.write_off
WHERE bill_id = $1::uuid OR (bill_id = ANY($2::uuid[]) AND created_at >= $3)
ORDER BY id`

// pendingWriteOffsSQL lists submitted write-off drafts that are not approved yet. Only
// status draft counts, so rejected and withdrawn drafts are left out (BIL-C-085).
const pendingWriteOffsSQL = `SELECT id::text, bill_id::text, amount_cents, created_at
FROM billing.write_off_draft
WHERE status = 'draft' AND credit_gate_id IS NOT NULL
ORDER BY id`

// CumulativeCents implements WriteOffTotals.
func (PGWriteOffTotals) CumulativeCents(ctx context.Context, tx pgx.Tx, s WriteOffScope) (int64, error) {
	if tx == nil || s.BillID == "" || s.ClientID == "" || s.Since.IsZero() {
		return 0, fmt.Errorf("write-off totals: incomplete scope: %w", ErrInvalidState)
	}
	var payer string
	if err := tx.QueryRow(ctx, lockBillSQL, s.BillID).Scan(&payer); err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isInvalidText(err) {
			return 0, fmt.Errorf("write-off totals: bill: %w", ErrNotFound)
		}
		return 0, fmt.Errorf("write-off totals: lock bill: %w", err)
	}
	// The scope's client must be the bill's payer, or the 30-day sum would read the wrong
	// client's bills.
	if payer != s.ClientID {
		return 0, fmt.Errorf("write-off totals: client is not the bill's payer: %w", ErrInvalidState)
	}
	bills, err := textColumn(ctx, tx, clientBillsSQL, payer)
	if err != nil {
		return 0, fmt.Errorf("write-off totals: client bills: %w", err)
	}
	clientBill := map[string]bool{}
	for _, b := range bills {
		clientBill[b] = true
	}

	counted := map[string]bool{s.ExcludeID: true}
	var total int64
	add := func(cents int64) error {
		if cents <= 0 || total > math.MaxInt64-cents {
			return fmt.Errorf("write-off totals: bad amount: %w", ErrInvalidState)
		}
		total += cents
		return nil
	}

	rows, err := tx.Query(ctx, approvedWriteOffsSQL, s.BillID, bills, s.Since)
	if err != nil {
		return 0, fmt.Errorf("write-off totals: approved: %w", err)
	}
	approvedErr := func() error {
		defer rows.Close()
		for rows.Next() {
			var id string
			var cents int64
			if err := rows.Scan(&id, &cents); err != nil {
				return err
			}
			if counted[id] {
				continue
			}
			counted[id] = true
			if err := add(cents); err != nil {
				return err
			}
		}
		return rows.Err()
	}()
	if approvedErr != nil {
		return 0, fmt.Errorf("write-off totals: approved: %w", approvedErr)
	}

	pending, err := tx.Query(ctx, pendingWriteOffsSQL)
	if err != nil {
		return 0, fmt.Errorf("write-off totals: pending: %w", err)
	}
	pendingErr := func() error {
		defer pending.Close()
		for pending.Next() {
			var id, bill string
			var cents int64
			var at time.Time
			if err := pending.Scan(&id, &bill, &cents, &at); err != nil {
				return err
			}
			if counted[id] {
				continue
			}
			if bill != s.BillID && (!clientBill[bill] || at.Before(s.Since)) {
				continue
			}
			counted[id] = true
			if err := add(cents); err != nil {
				return err
			}
		}
		return pending.Err()
	}()
	if pendingErr != nil {
		return 0, fmt.Errorf("write-off totals: pending: %w", pendingErr)
	}
	return total, nil
}

// textColumn runs sql and returns its single text column.
func textColumn(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]string, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// isInvalidText reports a malformed uuid (22P02).
func isInvalidText(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
