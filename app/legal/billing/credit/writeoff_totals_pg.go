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
// write-offs (billing.write_off) and write-offs still waiting on a pending gate whose
// subject is a write-off (the G-B5 gate or the managing lawyer's second approval). The
// pending amount is the gate's stored payload amount. Anything it cannot count fails closed.
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

// pendingWriteOffsSQL lists the pending gates on write-offs with the bill id and cents of
// their stored payload. A NULL payload is returned as such and fails the sum closed.
const pendingWriteOffsSQL = `SELECT payload_id::text,
  payload_jcs IS NOT NULL,
  COALESCE(convert_from(payload_jcs, 'UTF8')::jsonb ->> 'bill_id', ''),
  COALESCE((convert_from(payload_jcs, 'UTF8')::jsonb ->> 'amount_cents')::bigint, -1),
  created_at
FROM gate.approval_request
WHERE payload_type = $1 AND status = 'pending'
ORDER BY payload_id, created_at`

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

	pending, err := tx.Query(ctx, pendingWriteOffsSQL, SubjectWriteOff)
	if err != nil {
		return 0, fmt.Errorf("write-off totals: pending: %w", err)
	}
	pendingErr := func() error {
		defer pending.Close()
		for pending.Next() {
			var id, bill string
			var hasPayload bool
			var cents int64
			var at time.Time
			if err := pending.Scan(&id, &hasPayload, &bill, &cents, &at); err != nil {
				return err
			}
			if counted[id] {
				continue
			}
			if !hasPayload {
				return fmt.Errorf("pending gate on write-off %s has no stored payload: %w", id, ErrInvalidState)
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
