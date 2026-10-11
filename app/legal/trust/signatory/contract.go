// CONTRACT: app/legal/trust/signatory. The shape of this file is frozen after wave 0;
// changes are additive only and need a docs/QUESTIONS.md entry. Bodies live in the T-039L
// subtasks.

package signatory

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors.
var (
	ErrNotFound = errors.New("signatory: not found")
	// ErrNotEligible: the user is not an active licensee and has no bond evidence recorded.
	ErrNotEligible = errors.New("signatory: user is not eligible to sign")
	// ErrLastSignatory: the change would leave the account with no active signatory.
	ErrLastSignatory = errors.New("signatory: account needs at least one active signatory")
	// ErrSelfApproval: the preparer tried to approve their own change.
	ErrSelfApproval = errors.New("signatory: preparer cannot approve")
	// ErrAgentApproval: an agent principal tried to approve.
	ErrAgentApproval = errors.New("signatory: agent cannot approve")
	// ErrInvalidState: the change is not pending.
	ErrInvalidState = errors.New("signatory: invalid state")
)

// Kind is the basis of a signatory's authority.
type Kind string

// Signatory kinds.
const (
	Licensee          Kind = "licensee"
	BondedNonLicensee Kind = "bonded_non_licensee"
)

// Status is a signatory's current standing.
type Status string

// Signatory statuses. A frozen signatory cannot sign until the bank change is done.
const (
	StatusActive  Status = "active"
	StatusFrozen  Status = "frozen"
	StatusRemoved Status = "removed"
)

// Signatory is one user with signing authority on one trust account.
type Signatory struct {
	AccountID    string
	UserID       string
	Kind         Kind
	BondEvidence string // document reference; required for BondedNonLicensee
	Status       Status
	Since        time.Time
}

// User is the slice of the user directory this module reads.
type User struct {
	ID       string
	Licensee bool
	Active   bool // false once the user has left the firm
}

// Directory reads users owned by the identity module.
type Directory interface {
	User(ctx context.Context, id string) (User, error)
}

// ChangeStatus is the lifecycle state of a change request.
type ChangeStatus string

// Change statuses.
const (
	ChangePending  ChangeStatus = "pending"
	ChangeApproved ChangeStatus = "approved"
)

// Change adds and removes signatories on one account. It applies only on approval.
type Change struct {
	ID         string
	AccountID  string
	Add        []Signatory
	Remove     []string // user ids
	PreparedBy string
	Status     ChangeStatus
	ApprovedBy string
	ApprovedAt *time.Time
}

// Approver is who approves a change.
type Approver struct {
	UserID  string
	IsAgent bool
}

// Service manages signing authority on trust accounts.
type Service interface {
	List(ctx context.Context, accountID string) ([]Signatory, error)
	// CanSign reports whether userID is an active signatory on accountID.
	CanSign(ctx context.Context, accountID, userID string) (bool, error)
	// Propose validates eligibility and records a pending change.
	Propose(ctx context.Context, c Change) (Change, error)
	// Approve applies a pending change. The approver must be a licensee other than the preparer.
	Approve(ctx context.Context, id string, by Approver) (Change, error)
	// Freeze immediately freezes the user's authority on every account, for example when the
	// user leaves the firm. It needs no approval and returns the number of accounts affected.
	Freeze(ctx context.Context, userID string) (int, error)
}
