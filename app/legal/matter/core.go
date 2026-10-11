package matter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/outbox"
)

// pgUniqueViolation is the Postgres SQLSTATE for a unique constraint violation.
const pgUniqueViolation = "23505"

// Store creates matters and their teams in matter.matter and matter.matter_member. It reads
// contacts only through party.Contacts; it never joins party tables.
type Store struct {
	db       db.DB
	contacts party.Contacts
	// Events receives matter.opened and matter.stage_changed in the write's tx. Boot sets
	// it; while nil, Open and ChangeStage refuse (the event would be lost).
	Events outbox.Emitter
}

var _ Matters = (*Store)(nil)

// NewStore returns a Store on pool. contacts checks that a matter's client exists.
func NewStore(pool db.DB, contacts party.Contacts) *Store {
	return &Store{db: pool, contacts: contacts}
}

// validateMatter checks m and returns its visibility with the default applied.
func validateMatter(m NewMatter) (Visibility, error) {
	if m.Number == "" || m.ShortName == "" || m.ClientID == "" || m.PracticeAreaID == "" || m.ResponsibleLawyerID == "" {
		return "", fmt.Errorf("missing field: %w", ErrInvalid)
	}
	vis := m.Visibility
	if vis == "" {
		vis = FirmOpen
	}
	switch vis {
	case FirmOpen, TeamOnly, Restricted:
		return vis, nil
	default:
		return "", fmt.Errorf("visibility %q: %w", vis, ErrInvalid)
	}
}

// validateMember checks m and returns its role with the default applied.
func validateMember(m NewMember) (MemberRole, error) {
	if m.MatterID == "" || m.UserID == "" || m.GrantedBy == "" {
		return "", fmt.Errorf("missing field: %w", ErrInvalid)
	}
	if m.UserID == m.GrantedBy {
		return "", ErrSelfGrant
	}
	role := m.Role
	if role == "" {
		role = RoleMember
	}
	switch role {
	case RoleResponsible, RoleOriginating, RoleMember, RoleStaff, RoleReviewer:
		return role, nil
	default:
		return "", fmt.Errorf("role %q: %w", role, ErrInvalid)
	}
}

const insertMatterSQL = `INSERT INTO matter.matter
  (number, short_name, client_id, practice_area_id, responsible_lawyer_id, visibility, retention_class, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, '')::uuid, NULLIF($8, '')::uuid)
RETURNING id::text, status, open_date, created_at, row_version`

// Create implements Matters. A new matter is pending and has no open date.
func (s *Store) Create(ctx context.Context, m NewMatter) (Matter, error) {
	vis, err := validateMatter(m)
	if err != nil {
		return Matter{}, fmt.Errorf("create matter: %w", err)
	}
	if _, err := s.contacts.Get(ctx, m.ClientID); err != nil {
		if errors.Is(err, party.ErrNotFound) {
			return Matter{}, fmt.Errorf("create matter: %w", ErrUnknownClient)
		}
		return Matter{}, fmt.Errorf("create matter: client: %w", err)
	}
	out := Matter{
		Number:              m.Number,
		ShortName:           m.ShortName,
		ClientID:            m.ClientID,
		PracticeAreaID:      m.PracticeAreaID,
		ResponsibleLawyerID: m.ResponsibleLawyerID,
		Visibility:          vis,
		RetentionClass:      DefaultRetentionClass,
	}
	err = s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var (
			id     string
			status string
			open   *time.Time
		)
		if err := tx.QueryRow(ctx, insertMatterSQL,
			m.Number, m.ShortName, string(m.ClientID), m.PracticeAreaID, m.ResponsibleLawyerID,
			string(vis), DefaultRetentionClass, m.CreatedBy,
		).Scan(&id, &status, &open, &out.CreatedAt, &out.RowVersion); err != nil {
			return mapErr(err, ErrDuplicate)
		}
		out.ID = ID(id)
		out.Status = Status(status)
		if open != nil {
			out.OpenDate = *open
		}
		return nil
	})
	if err != nil {
		return Matter{}, fmt.Errorf("create matter: %w", err)
	}
	return out, nil
}

const insertMemberSQL = `INSERT INTO matter.matter_member (matter_id, user_id, matter_role, granted_by)
SELECT m.id, $2::uuid, $3, $4::uuid FROM matter.matter m WHERE m.id = $1::uuid
RETURNING id::text`

// AddMember implements Matters. The granter is never the member (CHECK in matter_member).
func (s *Store) AddMember(ctx context.Context, m NewMember) (Member, error) {
	role, err := validateMember(m)
	if err != nil {
		return Member{}, fmt.Errorf("add member: %w", err)
	}
	out := Member{MatterID: m.MatterID, UserID: m.UserID, Role: role, GrantedBy: m.GrantedBy}
	err = s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx, insertMemberSQL, string(m.MatterID), m.UserID, string(role), m.GrantedBy).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("matter %s: %w", m.MatterID, ErrNotFound)
			}
			return mapErr(err, ErrInvalid)
		}
		out.ID = MemberID(id)
		return nil
	})
	if err != nil {
		return Member{}, fmt.Errorf("add member: %w", err)
	}
	return out, nil
}

// mapErr turns a unique violation into dup; other errors pass through wrapped.
func mapErr(err, dup error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == pgUniqueViolation {
		return dup
	}
	return fmt.Errorf("insert: %w", err)
}
