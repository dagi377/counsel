// CONTRACT: app/legal/matter. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-010b subtasks (core.go, query.go).
// Source: migrations/0001_init.sql (matter.matter, matter.matter_member).

package matter

import (
	"context"
	"errors"
	"time"

	"github.com/dagi377/counsel/app/legal/party"
)

// ID is a matter.matter id.
type ID string

// MemberID is a matter.matter_member id.
type MemberID string

// Status is a matter's lifecycle status.
type Status string

// Matter statuses.
const (
	Pending  Status = "pending"
	Open     Status = "open"
	Closing  Status = "closing"
	Closed   Status = "closed"
	Archived Status = "archived"
)

// Visibility says who on the firm can see a matter before walls apply.
type Visibility string

// Visibilities.
const (
	FirmOpen   Visibility = "firm_open"
	TeamOnly   Visibility = "team_only"
	Restricted Visibility = "restricted"
)

// MemberRole is a member's role on the matter team.
type MemberRole string

// Member roles.
const (
	RoleResponsible MemberRole = "responsible"
	RoleOriginating MemberRole = "originating"
	RoleMember      MemberRole = "member"
	RoleStaff       MemberRole = "staff"
	RoleReviewer    MemberRole = "reviewer"
)

// NewMatter is what Create needs. Names and descriptions are client data: never log them.
type NewMatter struct {
	Number              string
	ShortName           string
	ClientID            party.ContactID
	PracticeAreaID      string
	ResponsibleLawyerID string // core.app_user id
	Visibility          Visibility
	CreatedBy           string // core.app_user id
}

// Matter is one matter.matter row as the query API returns it.
type Matter struct {
	ID                  ID
	Number              string
	ShortName           string
	ClientID            party.ContactID
	PracticeAreaID      string
	ResponsibleLawyerID string
	Status              Status
	Visibility          Visibility
	OpenDate            time.Time // zero while pending; a date in America/Toronto
	RetentionClass      string
	CreatedAt           time.Time
	RowVersion          int

	// Additive (Q-TAX-FIELDS): place-of-supply facts, empty when not recorded.
	// CourtProvince is the province whose court or tribunal has the litigation (BIL-T-012);
	// PropertyProvince is where the real property the matter relates to is (BIL-T-011).
	CourtProvince    string
	PropertyProvince string

	// RateTableID (additive, AGENT-PROD-3) is the matter's own rate table
	// (matter.rate_table_id); empty means the firm's default table applies.
	RateTableID string
}

// NewMember adds a user to a matter team. GrantedBy never equals UserID.
type NewMember struct {
	MatterID  ID
	UserID    string
	Role      MemberRole
	GrantedBy string
}

// Member is one matter.matter_member row.
type Member struct {
	ID        MemberID
	MatterID  ID
	UserID    string
	Role      MemberRole
	GrantedBy string
}

// Filter narrows List. Empty fields match everything.
type Filter struct {
	ClientID party.ContactID
	Status   Status
	MemberID string // only matters with this user on the team
}

// Sentinel errors.
var (
	ErrNotFound      = errors.New("matter: not found")
	ErrInvalid       = errors.New("matter: invalid")
	ErrDuplicate     = errors.New("matter: number already used")
	ErrSelfGrant     = errors.New("matter: member cannot grant themselves")
	ErrUnknownClient = errors.New("matter: unknown client")
)

// DefaultRetentionClass is the class a new matter gets.
const DefaultRetentionClass = "RC-FILE-15"

// Matters creates matters and their teams (T-010b.1).
type Matters interface {
	Create(ctx context.Context, m NewMatter) (Matter, error)
	AddMember(ctx context.Context, m NewMember) (Member, error)
}

// Query is the only way other modules read matters (T-010b.3, ARC-I-041).
type Query interface {
	Get(ctx context.Context, id ID) (Matter, error)
	List(ctx context.Context, f Filter) ([]Matter, error)
	Members(ctx context.Context, id ID) ([]Member, error)
}

// Service is everything app/legal/matter provides.
type Service interface {
	Matters
	Query
}
