package trust

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
)

// TxRunner runs fn in one transaction; kernel/db's pool implements it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// Page sizes the Postgres list readers use when a filter's Limit is out of range.
const (
	DefaultListLimit = 50
	MaxListLimit     = 200
)

// ListLimit bounds a page size: zero or less is DefaultListLimit, more than MaxListLimit is
// MaxListLimit.
func ListLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultListLimit
	case n > MaxListLimit:
		return MaxListLimit
	}
	return n
}

// PGLists is Lists over trust.trust_account and trust.client_ledger. It also reads one
// ledger (Ledger), which the withdrawal request route uses to resolve a ledger's account and
// matter. Every read runs in its own transaction on DB.
type PGLists struct {
	DB TxRunner
}

var _ Lists = PGLists{}

const listAccountsSQL = `SELECT id::text, type, institution, account_no_last4, title,
  coalesce(for_matter_id::text, ''), opened_on
FROM trust.trust_account ORDER BY id`

// ListAccounts implements Lists. The full account number is never read.
func (p PGLists) ListAccounts(ctx context.Context) ([]Account, error) {
	var out []Account
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, listAccountsSQL)
		if err != nil {
			return fmt.Errorf("select: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var a Account
			var id, typ string
			if err := rows.Scan(&id, &typ, &a.Institution, &a.AccountLast4, &a.Title, &a.ForMatterID, &a.OpenedOn); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			a.ID, a.Type = AccountID(id), AccountType(typ)
			out = append(out, a)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list trust accounts: %w", err)
	}
	return out, nil
}

// listLedgersSQL pages client ledgers by id. Empty filters are NULL, so the uuid casts never
// see an empty string.
const listLedgersSQL = `SELECT id::text, account_id::text, matter_id::text, client_id::text,
  protected_cents, coalesce(protected_purpose, ''), minimum_balance_cents
FROM trust.client_ledger
WHERE (NULLIF($1, '')::uuid IS NULL OR matter_id = NULLIF($1, '')::uuid)
  AND (NULLIF($2, '')::uuid IS NULL OR account_id = NULLIF($2, '')::uuid)
  AND (NULLIF($3, '')::uuid IS NULL OR id > NULLIF($3, '')::uuid)
ORDER BY id LIMIT $4`

// ListLedgers implements Lists. It returns ledger rows only: the balance (signed, never
// clamped) comes from Ledgers.Balance, the one source of a ledger's balance.
func (p PGLists) ListLedgers(ctx context.Context, f LedgerFilter) ([]Ledger, error) {
	var out []Ledger
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, listLedgersSQL, f.MatterID, f.AccountID, string(f.After), ListLimit(f.Limit))
		if err != nil {
			return fmt.Errorf("select: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			l, err := scanLedger(rows)
			if err != nil {
				return err
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list trust ledgers: %w", err)
	}
	return out, nil
}

// Ledger returns one client ledger, or ErrNotFound for an unknown, hidden or malformed id.
func (p PGLists) Ledger(ctx context.Context, id LedgerID) (Ledger, error) {
	var out Ledger
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = pgLedgerRepo{}.getLedger(ctx, tx, id)
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return Ledger{}, fmt.Errorf("ledger %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Ledger{}, fmt.Errorf("ledger %s: %w", id, err)
	}
	return out, nil
}

// NewLedgerReads returns the read side of a Postgres LedgerStore: Balance and Txns only. It
// posts nothing, so it needs no signatory guard or GL. Balance reports the true signed
// posted and available figures; nothing is clamped (BIL-I-010).
func NewLedgerReads(pool db.DB, clk clock.Clock) LedgerReads {
	return &LedgerStore{db: pool, repo: pgLedgerRepo{}, clk: clk}
}
