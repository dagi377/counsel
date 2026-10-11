package matter

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/party"
)

var _ Service = (*Store)(nil)

const matterColumns = `m.id::text, m.number, m.short_name, m.client_id::text, m.practice_area_id::text,
  m.responsible_lawyer_id::text, m.status, m.visibility, m.open_date, m.retention_class,
  m.created_at, m.row_version, coalesce(btrim(m.court_province), ''), coalesce(btrim(m.property_province), '')`

// rowScanner is the Scan half of pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanMatter reads one row selected with matterColumns.
func scanMatter(r rowScanner) (Matter, error) {
	var (
		m                              Matter
		id, client, status, visibility string
		open                           *time.Time
	)
	if err := r.Scan(&id, &m.Number, &m.ShortName, &client, &m.PracticeAreaID,
		&m.ResponsibleLawyerID, &status, &visibility, &open, &m.RetentionClass,
		&m.CreatedAt, &m.RowVersion, &m.CourtProvince, &m.PropertyProvince); err != nil {
		return Matter{}, err
	}
	m.ID = ID(id)
	m.ClientID = party.ContactID(client)
	m.Status = Status(status)
	m.Visibility = Visibility(visibility)
	if open != nil {
		m.OpenDate = *open
	}
	return m, nil
}

// Get implements Query.
// rule: ARC-I-041
func (s *Store) Get(ctx context.Context, id ID) (Matter, error) {
	if id == "" {
		return Matter{}, fmt.Errorf("get matter: %w", ErrInvalid)
	}
	var out Matter
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		m, err := scanMatter(tx.QueryRow(ctx, `SELECT `+matterColumns+` FROM matter.matter m WHERE m.id = $1::uuid`, string(id)))
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("matter %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("select: %w", err)
		}
		out = m
		return nil
	})
	if err != nil {
		return Matter{}, fmt.Errorf("get matter: %w", err)
	}
	return out, nil
}

// listSQL builds the List query for f and its arguments. Empty filter fields match everything.
func listSQL(f Filter) (string, []any) {
	var (
		where []string
		args  []any
	)
	arg := func(v string) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if f.ClientID != "" {
		where = append(where, "m.client_id = "+arg(string(f.ClientID))+"::uuid")
	}
	if f.Status != "" {
		where = append(where, "m.status = "+arg(string(f.Status)))
	}
	if f.MemberID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM matter.matter_member mm WHERE mm.matter_id = m.id AND mm.user_id = "+
			arg(f.MemberID)+"::uuid AND (mm.ends_on IS NULL OR mm.ends_on >= CURRENT_DATE))")
	}
	q := `SELECT ` + matterColumns + ` FROM matter.matter m`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	return q + " ORDER BY m.number", args
}

// List implements Query.
// rule: ARC-I-041
func (s *Store) List(ctx context.Context, f Filter) ([]Matter, error) {
	if f.Status != "" {
		switch f.Status {
		case Pending, Open, Closing, Closed, Archived:
		default:
			return nil, fmt.Errorf("list matters: status %q: %w", f.Status, ErrInvalid)
		}
	}
	q, args := listSQL(f)
	var out []Matter
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, q, args...)
		if err != nil {
			return fmt.Errorf("select: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMatter(rows)
			if err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list matters: %w", err)
	}
	return out, nil
}

const existsMatterSQL = `SELECT EXISTS (SELECT 1 FROM matter.matter WHERE id = $1::uuid)`

const membersSQL = `SELECT mm.id::text, mm.matter_id::text, mm.user_id::text, mm.matter_role, mm.granted_by::text
FROM matter.matter_member mm
WHERE mm.matter_id = $1::uuid AND mm.user_id IS NOT NULL AND (mm.ends_on IS NULL OR mm.ends_on >= CURRENT_DATE)
ORDER BY mm.granted_at, mm.id`

// Members implements Query. It returns the matter's current user members; team grants and
// ended memberships are left out.
// rule: ARC-I-041
func (s *Store) Members(ctx context.Context, id ID) ([]Member, error) {
	if id == "" {
		return nil, fmt.Errorf("matter members: %w", ErrInvalid)
	}
	var out []Member
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, existsMatterSQL, string(id)).Scan(&ok); err != nil {
			return fmt.Errorf("select: %w", err)
		}
		if !ok {
			return fmt.Errorf("matter %s: %w", id, ErrNotFound)
		}
		rows, err := tx.Query(ctx, membersSQL, string(id))
		if err != nil {
			return fmt.Errorf("select: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var mid, matterID, role string
			var m Member
			if err := rows.Scan(&mid, &matterID, &m.UserID, &role, &m.GrantedBy); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			m.ID, m.MatterID, m.Role = MemberID(mid), ID(matterID), MemberRole(role)
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("matter members: %w", err)
	}
	return out, nil
}
