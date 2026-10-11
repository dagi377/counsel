package recon

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

// PGStatements is the Postgres Statements port (HTTP-18b, http_write.go) over trust.bank_import: it reads the
// imported statement's closing balance in cents (migrations/5530_bank_import_balances.sql).
type PGStatements struct {
	conn Querier
}

// accountIDPattern matches a trust.trust_account id (uuid text form).
var accountIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// NewPGStatements returns a PGStatements that reads on conn (db.Pool satisfies Querier).
func NewPGStatements(conn Querier) *PGStatements { return &PGStatements{conn: conn} }

// sqlStatementClosing selects the closing balance of the latest import whose statement
// period covers the month's last day. An import with no stored closing balance (made before
// 5530) does not count.
const sqlStatementClosing = `SELECT closing_cents FROM trust.bank_import
WHERE account_id = $1::uuid AND period @> $2::date AND closing_cents IS NOT NULL
ORDER BY imported_at DESC, id DESC
LIMIT 1`

// Closing implements Statements: the closing balance of the account's imported statement for
// p, or ErrNotFound. A malformed account id or period is ErrNotFound.
func (s *PGStatements) Closing(ctx context.Context, accountID string, p Period) (int64, error) {
	if !accountIDPattern.MatchString(accountID) || p.Year < 1 || p.Month < time.January || p.Month > time.December {
		return 0, fmt.Errorf("statement closing %s %d-%02d: %w", accountID, p.Year, p.Month, ErrNotFound)
	}
	// The statement for the month is the one that covers its last day.
	lastDay := time.Date(p.Year, p.Month+1, 0, 0, 0, 0, 0, time.UTC)
	var closing int64
	err := s.conn.QueryRow(ctx, sqlStatementClosing, accountID, lastDay).Scan(&closing)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("statement closing %s %d-%02d: %w", accountID, p.Year, p.Month, ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("statement closing %s %d-%02d: %w", accountID, p.Year, p.Month, err)
	}
	return closing, nil
}
