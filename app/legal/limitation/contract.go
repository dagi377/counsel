// CONTRACT: app/legal/limitation (T-031L). Frozen once on main; changes are additive only and
// need a docs/QUESTIONS.md entry. Source: table cal.limitation_assessment
// (spec/eng/a4-data-model.sql) and spec/legal/deadlines.json (Limitations Act, 2002: basic
// period from discovery, ultimate period from the act, suspensions). Bodies live in the
// T-031L subtasks (assess.go; calendar and day counting come from the rule packs).

// Package limitation records limitation assessments: a matter or lead, the claim facts, and
// the limitation deadlines computed from them. A computed deadline is a proposal until a
// person other than its preparer confirms it.
package limitation

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrNotFound means no assessment has the id (404).
	ErrNotFound = errors.New("limitation: assessment not found")
	// ErrOwner means the assessment names both or neither of matter and lead (422).
	ErrOwner = errors.New("limitation: exactly one of matter and lead is required")
	// ErrDates means discovery is before the act, or a date needed to compute is missing (422).
	ErrDates = errors.New("limitation: act and discovery dates are inconsistent or missing")
	// ErrNoPeriod means the claim type has no limitation period; nothing is computed.
	ErrNoPeriod = errors.New("limitation: claim type has no limitation period")
	// ErrSelfConfirm means the preparer, or an agent, tried to confirm (403).
	ErrSelfConfirm = errors.New("limitation: preparer cannot confirm")
	// ErrConfirmed means the assessment was already confirmed (409).
	ErrConfirmed = errors.New("limitation: already confirmed")
)

// SuspensionKind is why a period stops running.
type SuspensionKind string

// Suspension kinds.
const (
	SuspendMinority    SuspensionKind = "minority"
	SuspendIncapacity  SuspensionKind = "incapacity"
	SuspendConcealment SuspensionKind = "concealment"
	SuspendAgreement   SuspensionKind = "agreement"
)

// Suspension is one interval during which a period does not run. To nil means still running.
type Suspension struct {
	Kind SuspensionKind
	From time.Time
	To   *time.Time
}

// Assessment is one row of cal.limitation_assessment. Dates are America/Toronto calendar
// dates. Exactly one of MatterID and LeadID is set. DiscoveryFacts is client data: never log it.
type Assessment struct {
	ID                    string
	MatterID              string
	LeadID                string
	DefendantContactID    string
	ClaimType             string
	ActDate               *time.Time
	DiscoveryDate         *time.Time
	DiscoveryFacts        string
	Suspensions           []Suspension
	TollingAgreementDocID string
	BasicDeadlineID       string
	UltimateDeadlineID    string
	NoticeDeadlineIDs     []string
	PreparedBy            string // user id that prepared it (an agent's acting-for user)
	ConfirmedBy           string // empty until a different user confirms
}

// Period is one computed limitation date, before it becomes a deadline row.
type Period struct {
	Kind string // "basic", "ultimate" or a notice id
	Due  time.Time
}

// Calculator computes limitation periods from an assessment's facts. It is pure.
type Calculator interface {
	Compute(ctx context.Context, a Assessment) ([]Period, error)
}

// Store keeps assessments. Rows are corrected by update, never deleted.
type Store interface {
	Create(ctx context.Context, a Assessment) (Assessment, error)
	Get(ctx context.Context, id string) (Assessment, error)
	ForMatter(ctx context.Context, matterID string) ([]Assessment, error)
	// Confirm records the human confirmation. by must differ from PreparedBy.
	Confirm(ctx context.Context, id, by string) (Assessment, error)
}
