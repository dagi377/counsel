package matter

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SessionUserVar is the transaction-local setting the matter RLS policies read through
// core.current_user_id(). kernel/db Bind sets it per request; walls and visibility key off it.
const SessionUserVar = "counsel.user_id"

// AppRole is the database role the request path runs as. The matter policies apply to it;
// background roles are BYPASSRLS and audited.
const AppRole = "counsel_app"

// RLSTables are the matter tables walled by matter.can_see (migrations/0001_init.sql).
var RLSTables = []string{"matter.matter", "matter.note", "matter.fact"}

// ErrRLSMissing means a matter table lacks enabled and forced row-level security or a policy
// for AppRole. The app must not serve matter data from such a database.
var ErrRLSMissing = errors.New("matter: row-level security missing")

// RowQuerier is the subset of pgx.Tx and pgx.Conn that CheckRLS needs.
type RowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// CheckRLS reports ErrRLSMissing unless every table in RLSTables has row-level security
// enabled and forced and at least one policy that applies to AppRole. It fails closed: a
// table it cannot find counts as missing.
func CheckRLS(ctx context.Context, q RowQuerier) error {
	for _, tbl := range RLSTables {
		var enabled, forced bool
		var policies int
		err := q.QueryRow(ctx, `
SELECT c.relrowsecurity, c.relforcerowsecurity,
       (SELECT count(*) FROM pg_catalog.pg_policies p
         WHERE p.schemaname = n.nspname AND p.tablename = c.relname
           AND ($2 = ANY (p.roles) OR 'public' = ANY (p.roles)))
  FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
 WHERE c.oid = to_regclass($1)`, tbl, AppRole).Scan(&enabled, &forced, &policies)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check rls %s: not found: %w", tbl, ErrRLSMissing)
		}
		if err != nil {
			return fmt.Errorf("check rls %s: %w", tbl, err)
		}
		if !enabled || !forced || policies == 0 {
			return fmt.Errorf("check rls %s: enabled=%t forced=%t policies=%d: %w", tbl, enabled, forced, policies, ErrRLSMissing)
		}
	}
	return nil
}
