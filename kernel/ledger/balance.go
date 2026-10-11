package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Querier runs a single-row query. pgx.Tx, *pgx.Conn and *pgxpool.Pool satisfy it.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Balances reads account balances from gl.journal_line.
type Balances struct {
	Q Querier
}

// Statements Balance issues. Balance only reads.
const (
	sqlBalanceAccount = `SELECT id FROM gl.account WHERE code = $1`
	sqlBalanceSum     = `SELECT coalesce(sum(l.debit_cents - l.credit_cents), 0)::bigint
FROM gl.journal_line l
JOIN gl.journal_entry e ON e.id = l.entry_id
WHERE l.account_id = $1 AND e.entry_date <= $2`
)

// balanceStatements lists every statement Balance may run, for the read-only check.
var balanceStatements = []string{sqlBalanceAccount, sqlBalanceSum}

// Balance returns debits minus credits on account for entries dated on or before asOf.
// Reversals are ordinary entries, so a reversed entry nets to zero.
func (b Balances) Balance(ctx context.Context, account string, asOf Date) (Cents, error) {
	var id string
	if err := b.Q.QueryRow(ctx, sqlBalanceAccount, account).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("balance %q: %w", account, ErrUnknownAccount)
		}
		return 0, fmt.Errorf("balance %q: account lookup: %w", account, err)
	}
	date := time.Date(asOf.Year, asOf.Month, asOf.Day, 0, 0, 0, 0, time.UTC)
	var sum int64
	if err := b.Q.QueryRow(ctx, sqlBalanceSum, id, date).Scan(&sum); err != nil {
		return 0, fmt.Errorf("balance %q: sum: %w", account, err)
	}
	return Cents(sum), nil
}
