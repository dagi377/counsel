package signatory

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
)

// CanSignTx reports, in the caller's tx, whether userID can sign on accountID today. It runs
// the same query and validity rule as PGService.CanSign: a grant that has started and not
// ended, held by a licensee or by a user whose bond has not expired. A malformed id cannot
// sign. Errors never carry the ids.
func CanSignTx(ctx context.Context, tx pgx.Tx, accountID, userID string) (bool, error) {
	today := civil(clock.Today(ctx))
	rs, err := scanGrants(ctx, tx, canSignSQL, accountID, today, userID)
	if malformed(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("can sign: %w", err)
	}
	for _, r := range rs {
		if r.canSign(today) {
			return true, nil
		}
	}
	return false, nil
}

// scanGrants reads trust.signatory rows with sql in tx.
func scanGrants(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]row, error) {
	rs, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.UserID, &r.Licensee, &r.Bonded, &r.BondExpiry, &r.ValidFrom, &r.ValidTo); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rs.Err()
}
