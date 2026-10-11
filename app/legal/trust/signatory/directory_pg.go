package signatory

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
)

// PGDirectory reads users from core.app_user, the identity module's table. It only reads, in
// its own transaction.
type PGDirectory struct {
	db db.DB
}

var _ Directory = (*PGDirectory)(nil)

// NewPGDirectory returns a PGDirectory on pool.
func NewPGDirectory(pool db.DB) *PGDirectory {
	return &PGDirectory{db: pool}
}

// directoryUserSQL maps one core.app_user row to a User. A licensee is a human lawyer or
// paralegal in good standing; a suspended or unknown licence does not count. A user is
// active while their account is active and not offboarded, archived or past its expiry date
// (a business date in America/Toronto).
const directoryUserSQL = `SELECT u.id::text,
  (u.principal_kind = 'human' AND u.licence_type IN ('lawyer', 'paralegal')
    AND u.licence_status = 'good_standing') AS licensee,
  (u.status = 'active' AND u.offboarded_at IS NULL AND u.archived_at IS NULL
    AND (u.expires_on IS NULL OR u.expires_on >= (now() AT TIME ZONE 'America/Toronto')::date)) AS active
FROM core.app_user u
WHERE u.id = $1::uuid`

const pgInvalidTextRepresentation = "22P02"

// User implements Directory. An unknown or malformed id returns ErrNotFound; any other read
// error is wrapped and returned. Errors never carry the id.
func (d *PGDirectory) User(ctx context.Context, id string) (User, error) {
	var u User
	err := d.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, directoryUserSQL, id).Scan(&u.ID, &u.Licensee, &u.Active)
	})
	if err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == pgInvalidTextRepresentation) {
			return User{}, fmt.Errorf("directory user: %w", ErrNotFound)
		}
		return User{}, fmt.Errorf("directory user: %w", err)
	}
	return u, nil
}
