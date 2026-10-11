package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Statements the period code issues. Closing marks a month closed; it never reopens one.
// rule: BIL-I-025
const (
	sqlClosePeriod = `INSERT INTO gl.period (month, status, closed_by, closed_at)
VALUES ($1, 'closed', $2::uuid, now())
ON CONFLICT (month) DO UPDATE SET status = 'closed', closed_by = EXCLUDED.closed_by, closed_at = EXCLUDED.closed_at
WHERE gl.period.status = 'open'`
	sqlPeriodClosed = `SELECT EXISTS (SELECT 1 FROM gl.period WHERE month = $1 AND status = 'closed')`
)

// months returns the first day of every month p covers. A period is whole months: From is
// the first day of a month, To is the last day of a month, and From is not after To.
func months(p Period) ([]Date, error) {
	from := time.Date(p.From.Year, p.From.Month, p.From.Day, 0, 0, 0, 0, time.UTC)
	to := time.Date(p.To.Year, p.To.Month, p.To.Day, 0, 0, 0, 0, time.UTC)
	if from.Year() != p.From.Year || from.Month() != p.From.Month || from.Day() != p.From.Day ||
		to.Year() != p.To.Year || to.Month() != p.To.Month || to.Day() != p.To.Day {
		return nil, fmt.Errorf("period %s..%s: not a valid date: %w", p.From, p.To, ErrInvalidEntry)
	}
	if from.Day() != 1 || to.AddDate(0, 0, 1).Day() != 1 || to.Before(from) {
		return nil, fmt.Errorf("period %s..%s: not whole months: %w", p.From, p.To, ErrInvalidEntry)
	}
	var out []Date
	for m := from; !m.After(to); m = m.AddDate(0, 1, 0) {
		out = append(out, Date{Year: m.Year(), Month: m.Month(), Day: 1})
	}
	return out, nil
}

func monthStart(d Date) time.Time {
	return time.Date(d.Year, d.Month, 1, 0, 0, 0, 0, time.UTC)
}

// ClosePeriod marks every month of p closed inside tx. After commit the journal_entry_period
// trigger refuses any entry dated inside p; corrections post in the current period, e.g.
// with Reverse. Closing a month that is already closed changes nothing. gl.period is kept
// per month for the whole ledger, so book is required but every book shares the close.
// rule: BIL-I-025
func (g *PG) ClosePeriod(ctx context.Context, tx pgx.Tx, book string, p Period) error {
	if book == "" {
		return fmt.Errorf("close period: book required: %w", ErrInvalidEntry)
	}
	ms, err := months(p)
	if err != nil {
		return err
	}
	if g.PostedBy == nil {
		return fmt.Errorf("close period %s..%s: %w", p.From, p.To, ErrNoPoster)
	}
	by, err := g.PostedBy(ctx)
	if err != nil || by == "" {
		return fmt.Errorf("close period %s..%s: %w", p.From, p.To, errors.Join(ErrNoPoster, err))
	}
	for _, m := range ms {
		if _, err := tx.Exec(ctx, sqlClosePeriod, monthStart(m), by); err != nil {
			return fmt.Errorf("close period %s: %w", m, err)
		}
	}
	return nil
}

// CheckOpen returns ErrPeriodClosed when d falls in a closed month. Callers use it to refuse
// a posting before the database trigger does, with an error they can match.
// rule: BIL-I-025
func (g *PG) CheckOpen(ctx context.Context, tx pgx.Tx, d Date) error {
	var closed bool
	if err := tx.QueryRow(ctx, sqlPeriodClosed, monthStart(d)).Scan(&closed); err != nil {
		return fmt.Errorf("check period %s: %w", d, err)
	}
	if closed {
		return fmt.Errorf("date %s: %w", d, ErrPeriodClosed)
	}
	return nil
}
