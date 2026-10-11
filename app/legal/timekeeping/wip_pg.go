package timekeeping

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
)

// workDateZone is the business time zone a work_date calendar day is read in.
const workDateZone = "America/Toronto"

// PricedEntry is a time entry with the rate and line amount resolved when it was recorded,
// in integer cents (timekeeping.time_entry.rate_cents and amount_cents).
type PricedEntry struct {
	Entry       TimeEntry
	RateCents   int64
	AmountCents int64
}

// PGWIP reads a matter's time entries and its unbilled WIP from timekeeping.time_entry.
type PGWIP struct {
	DB db.DB
}

const matterTimeSQL = `SELECT e.id::text, e.matter_id::text, e.user_id::text, e.work_date::text,
  e.minutes, e.billed_minutes, coalesce(e.activity_code_id::text, ''), e.narrative, e.billable,
  e.origin, e.status, coalesce(e.bill_id::text, ''), e.row_version, e.rate_cents, e.amount_cents
  FROM timekeeping.time_entry e
  WHERE e.matter_id = $1::uuid
  ORDER BY e.work_date, e.id`

// MatterTime lists every time entry of matterID, priced at its stored rate and amount, by
// work date then id. A matter with no entries has none.
func (p PGWIP) MatterTime(ctx context.Context, matterID string) ([]PricedEntry, error) {
	if p.DB == nil {
		return nil, fmt.Errorf("matter time %s: no database: %w", matterID, ErrNotFound)
	}
	loc, err := time.LoadLocation(workDateZone)
	if err != nil {
		return nil, fmt.Errorf("matter time %s: %w", matterID, err)
	}
	var out []PricedEntry
	err = p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, matterTimeSQL, matterID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				pe       PricedEntry
				e        = &pe.Entry
				workDate string
			)
			if err := rows.Scan(&e.ID, &e.MatterID, &e.UserID, &workDate, &e.Minutes, &e.BilledMinutes,
				&e.ActivityCode, &e.Narrative, &e.Billable, &e.Origin, &e.State, &e.BillID, &e.RowVersion,
				&pe.RateCents, &pe.AmountCents); err != nil {
				return err
			}
			// A work date is a calendar day in Toronto, not a UTC instant.
			if e.WorkDate, err = time.ParseInLocation(time.DateOnly, workDate, loc); err != nil {
				return err
			}
			out = append(out, pe)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("matter time %s: %w", matterID, err)
	}
	return out, nil
}

// unbilledSQL selects the WIP a draft bill takes: approved, billable time on no bill, worked
// on or before the period end.
const unbilledSQL = `SELECT e.id::text FROM timekeeping.time_entry e
  WHERE e.matter_id = $1::uuid AND e.status = 'approved' AND e.billable AND e.bill_id IS NULL
    AND e.work_date <= $2::date
  ORDER BY e.work_date, e.id`

// ErrNoPeriodEnd means an unbilled WIP read had no period end.
var ErrNoPeriodEnd = errors.New("timekeeping: no period end")

// UnbilledEntryIDs lists the ids of matterID's approved, billable, unbilled time entries
// worked on or before periodTo.
func (p PGWIP) UnbilledEntryIDs(ctx context.Context, matterID string, periodTo clock.Date) ([]string, error) {
	if p.DB == nil {
		return nil, fmt.Errorf("unbilled wip %s: no database: %w", matterID, ErrNotFound)
	}
	if periodTo == (clock.Date{}) {
		return nil, fmt.Errorf("unbilled wip %s: %w", matterID, ErrNoPeriodEnd)
	}
	var out []string
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, unbilledSQL, matterID, periodTo.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("unbilled wip %s: %w", matterID, err)
	}
	return out, nil
}

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

// Unbilled and Claim read a matter's approved, unbilled work and put it on a draft bill, in
// the caller's tx (they do not use PGWIP.DB). Timekeeping owns these rows; billing reaches
// them only through this port.

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
