package timekeeping

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// WIPItem is one approved, unbilled time entry or expense. Amounts are int64 cents.
type WIPItem struct {
	Kind          string // "time" or "expense"
	ID            string
	Date          time.Time
	Narrative     string
	QuantityMilli int64 // billed hours*1000 for time, 1000 for an expense
	RateCents     int64
	AmountCents   int64
	Agency        bool // an expense paid as the client's agent
}

// SQL PGWIP runs. Tests script a pgx.Tx against these exact strings.
const (
	wipTimeSQL = `
SELECT id::text, work_date, narrative, billed_minutes::bigint * 1000 / 60, rate_cents, amount_cents
  FROM timekeeping.time_entry
 WHERE matter_id = $1::uuid AND status = 'approved' AND billable AND bill_id IS NULL
   AND ($2::date IS NULL OR work_date >= $2::date) AND work_date <= $3::date
 ORDER BY work_date, id`
	wipExpenseSQL = `
SELECT id::text, expense_date, coalesce(vendor, ''), amount_pre_tax_cents, is_agency
  FROM timekeeping.expense
 WHERE matter_id = $1::uuid AND status = 'approved' AND billable AND bill_id IS NULL
   AND ($2::date IS NULL OR expense_date >= $2::date) AND expense_date <= $3::date
 ORDER BY expense_date, id`
	wipClaimTimeSQL = `
UPDATE timekeeping.time_entry SET status = 'on_draft_bill', bill_id = $1::uuid
 WHERE id = ANY($2::uuid[]) AND status = 'approved' AND bill_id IS NULL`
	wipClaimExpenseSQL = `
UPDATE timekeeping.expense SET status = 'on_draft_bill', bill_id = $1::uuid
 WHERE id = ANY($2::uuid[]) AND status = 'approved' AND bill_id IS NULL`
)

// PGWIP reads a matter's approved, unbilled work and puts it on a draft bill, in the
// caller's tx. Timekeeping owns these rows; billing reaches them only through this port.
type PGWIP struct{}

// Unbilled returns the matter's approved, unbilled items dated in [from, to]; a zero from
// means from the oldest one. Time entries come first, then expenses.
func (PGWIP) Unbilled(ctx context.Context, tx pgx.Tx, matterID string, from, to time.Time) ([]WIPItem, error) {
	var lo *string
	if !from.IsZero() {
		s := from.Format(time.DateOnly)
		lo = &s
	}
	hi := to.Format(time.DateOnly)
	rows, err := tx.Query(ctx, wipTimeSQL, matterID, lo, hi)
	if err != nil {
		return nil, fmt.Errorf("unbilled time: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (WIPItem, error) {
		it := WIPItem{Kind: "time"}
		err := r.Scan(&it.ID, &it.Date, &it.Narrative, &it.QuantityMilli, &it.RateCents, &it.AmountCents)
		return it, err
	})
	if err != nil {
		return nil, fmt.Errorf("unbilled time: %w", err)
	}
	rows, err = tx.Query(ctx, wipExpenseSQL, matterID, lo, hi)
	if err != nil {
		return nil, fmt.Errorf("unbilled expenses: %w", err)
	}
	exp, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (WIPItem, error) {
		it := WIPItem{Kind: "expense", QuantityMilli: 1000}
		err := r.Scan(&it.ID, &it.Date, &it.Narrative, &it.AmountCents, &it.Agency)
		it.RateCents = it.AmountCents
		return it, err
	})
	if err != nil {
		return nil, fmt.Errorf("unbilled expenses: %w", err)
	}
	return append(out, exp...), nil
}

// Claim puts the items on draft bill billID. An item already claimed is an error, so two
// drafts never bill the same work.
func (PGWIP) Claim(ctx context.Context, tx pgx.Tx, billID string, items []WIPItem) error {
	var times, expenses []string
	for _, it := range items {
		if it.Kind == "time" {
			times = append(times, it.ID)
		} else {
			expenses = append(expenses, it.ID)
		}
	}
	for _, c := range []struct {
		sql string
		ids []string
	}{{wipClaimTimeSQL, times}, {wipClaimExpenseSQL, expenses}} {
		if len(c.ids) == 0 {
			continue
		}
		tag, err := tx.Exec(ctx, c.sql, billID, c.ids)
		if err != nil {
			return fmt.Errorf("claim for bill %s: %w", billID, err)
		}
		if tag.RowsAffected() != int64(len(c.ids)) {
			return fmt.Errorf("claim for bill %s: %w", billID, ErrBilledLocked)
		}
	}
	return nil
}
