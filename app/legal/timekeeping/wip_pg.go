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
