package trust

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// PGMatterAccounts lists the trust accounts that hold a client ledger for a matter, from
// trust.client_ledger.
type PGMatterAccounts struct {
	DB db.DB
}

// matterAccountsSQL lists open accounts only: a closed account takes no withdrawal.
const matterAccountsSQL = `SELECT DISTINCT l.account_id::text
  FROM trust.client_ledger l JOIN trust.trust_account a ON a.id = l.account_id
  WHERE l.matter_id = $1::uuid AND a.closed_on IS NULL
  ORDER BY 1`

// TrustAccountsOf lists matterID's open trust accounts, by id. A matter with no ledger has
// none.
func (p PGMatterAccounts) TrustAccountsOf(ctx context.Context, matterID string) ([]string, error) {
	if p.DB == nil {
		return nil, fmt.Errorf("trust accounts of %s: no database: %w", matterID, ErrNotFound)
	}
	var out []string
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, matterAccountsSQL, matterID)
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
		return nil, fmt.Errorf("trust accounts of %s: %w", matterID, err)
	}
	return out, nil
}
