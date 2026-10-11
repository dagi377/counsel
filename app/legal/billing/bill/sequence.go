package bill

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// billSeries is the core.number_series series that holds bill counters. Each prefix and
// year has its own row, keyed by period_key "PREFIX-YYYY", so every series restarts at 1
// each year and stays gapless on its own. Credit notes use their own series (T-023).
const billSeries = "bill"

// NextNumber issues the next bill number for prefix and year inside the caller's tx. The
// counter row is locked with SELECT ... FOR UPDATE until tx ends, so concurrent approvals
// queue, and a rolled-back tx gives its number back to the series (gapless).
//
// kernel/numseq is not used here because it takes the year from the clock; a bill's year
// is the year of its approval date in America/Toronto, which the caller passes in.
func NextNumber(ctx context.Context, tx pgx.Tx, prefix string, year int) (string, error) {
	// Validate prefix and year before touching the counter.
	if _, err := FormatNumber(prefix, year, 1); err != nil {
		return "", fmt.Errorf("next bill number: %w", err)
	}
	period := fmt.Sprintf("%s-%04d", prefix, year)
	// rule: BIL-C-120
	if _, err := tx.Exec(ctx,
		`INSERT INTO core.number_series (series, period_key) VALUES ($1, $2) ON CONFLICT (series, period_key) DO NOTHING`,
		billSeries, period); err != nil {
		return "", fmt.Errorf("next bill number %s: ensure counter: %w", period, err)
	}
	var n int64
	if err := tx.QueryRow(ctx,
		`SELECT next_value FROM core.number_series WHERE series = $1 AND period_key = $2 FOR UPDATE`,
		billSeries, period).Scan(&n); err != nil {
		return "", fmt.Errorf("next bill number %s: lock counter: %w", period, err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE core.number_series SET next_value = next_value + 1 WHERE series = $1 AND period_key = $2`,
		billSeries, period); err != nil {
		return "", fmt.Errorf("next bill number %s: advance counter: %w", period, err)
	}
	num, err := FormatNumber(prefix, year, int(n))
	if err != nil {
		return "", fmt.Errorf("next bill number %s: %w", period, err)
	}
	return num, nil
}
