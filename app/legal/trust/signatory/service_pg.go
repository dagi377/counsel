package signatory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
)

// PGService is the Postgres Service over trust.signatory. A row is one grant of signing
// authority on one account for the dates [valid_from, valid_to). Business dates are the
// kernel clock's (America/Toronto).
//
// valid_to is the first day the authority no longer holds: Freeze sets it to today, so a
// frozen signatory cannot sign from that moment on. The row is still listed (as frozen) on
// its valid_to day and drops out of List the day after.
type PGService struct {
	db db.DB
}

var _ Service = (*PGService)(nil)

// NewPGService returns a PGService on pool.
func NewPGService(pool db.DB) *PGService {
	return &PGService{db: pool}
}

// row is one trust.signatory row as the service reads it.
type row struct {
	UserID     string
	Licensee   bool
	Bonded     bool // bond_cents IS NOT NULL
	BondExpiry *time.Time
	ValidFrom  time.Time
	ValidTo    *time.Time
}

// signatorySQL selects the rows of an account that are current on $2: not ended before it.
const signatorySQL = `SELECT user_id::text, is_licensee, bond_cents IS NOT NULL, bond_expiry, valid_from, valid_to
FROM trust.signatory
WHERE account_id = $1::uuid AND (valid_to IS NULL OR valid_to >= $2::date)`

// canSignSQL narrows signatorySQL to one user.
const canSignSQL = signatorySQL + ` AND user_id = $3::uuid`

// freezeSQL ends every open grant of a user today. A grant that starts later ends on its
// first day (valid_to >= valid_from holds), so it never takes effect.
const freezeSQL = `UPDATE trust.signatory SET valid_to = GREATEST(valid_from, $2::date)
WHERE user_id = $1::uuid AND valid_to IS NULL`

// civil returns d as midnight UTC, the value pgx scans a date column into.
func civil(d clock.Date) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// canSign reports whether r grants authority on today: the grant has started, has not
// ended, and the user is a licensee or holds a bond that has not expired.
func (r row) canSign(today time.Time) bool {
	if r.ValidFrom.After(today) || (r.ValidTo != nil && !r.ValidTo.After(today)) {
		return false
	}
	return r.Licensee || (r.Bonded && (r.BondExpiry == nil || !r.BondExpiry.Before(today)))
}

// signatory maps r to a Signatory of accountID. A row that cannot sign today is frozen.
func (r row) signatory(accountID string, today time.Time) Signatory {
	s := Signatory{AccountID: accountID, UserID: r.UserID, Kind: BondedNonLicensee, Status: StatusFrozen, Since: r.ValidFrom}
	if r.Licensee {
		s.Kind = Licensee
	}
	if r.canSign(today) {
		s.Status = StatusActive
	} else if r.ValidTo != nil {
		s.Since = *r.ValidTo
	}
	return s
}

// malformed reports a uuid parse error: an id that cannot exist.
func malformed(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == pgInvalidTextRepresentation
}

func (s *PGService) rows(ctx context.Context, sql string, args ...any) ([]row, error) {
	var out []row
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rs, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var r row
			if err := rs.Scan(&r.UserID, &r.Licensee, &r.Bonded, &r.BondExpiry, &r.ValidFrom, &r.ValidTo); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rs.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// List implements Service: the account's current signatories, active or frozen, by user id.
// An unknown or malformed account id has none.
func (s *PGService) List(ctx context.Context, accountID string) ([]Signatory, error) {
	today := civil(clock.Today(ctx))
	rs, err := s.rows(ctx, signatorySQL, accountID, today)
	if malformed(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list signatories: %w", err)
	}
	out := make([]Signatory, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.signatory(accountID, today))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out, nil
}

// CanSign implements Service: true only when a grant of userID on accountID is valid today
// and the user is a licensee or holds an unexpired bond. Errors never carry the ids.
func (s *PGService) CanSign(ctx context.Context, accountID, userID string) (bool, error) {
	today := civil(clock.Today(ctx))
	rs, err := s.rows(ctx, canSignSQL, accountID, today, userID)
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

// Freeze implements Service: it ends every open grant of userID today and returns how many
// it ended. It needs no approval. An unknown or malformed user id freezes nothing.
func (s *PGService) Freeze(ctx context.Context, userID string) (int, error) {
	today := civil(clock.Today(ctx))
	var tag pgconn.CommandTag
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		tag, err = tx.Exec(ctx, freezeSQL, userID, today)
		return err
	})
	if malformed(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("freeze signatory: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
