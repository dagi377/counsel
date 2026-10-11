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

	// Additive (Q-T-031L.4). DiscoveryPresumed records that discovery is the day of the act
	// or omission under the s.5(2) presumption; when false, DiscoveryDate is the proved date.
	DiscoveryPresumed bool
	// NoLimit names a claim with no limitation period (s.16, s.17); empty for none.
	NoLimit NoLimitKind
	// ContributionServedDate, when set, makes this a claim for contribution and indemnity:
	// s.18 deems the act or omission to be the day the claimant wrongdoer was served.
	ContributionServedDate *time.Time
}

// Additive sentinel errors (Q-T-031L.4).
var (
	// ErrUnverified means the deadline.ontario_pack flag is off, so nothing is computed.
	ErrUnverified = errors.New("limitation: ontario limitation rules not verified")
	// ErrSuspended means every period is suspended with no end date; recompute when it ends.
	ErrSuspended = errors.New("limitation: periods suspended with no end date")
)

// NoLimitKind names a claim the Limitations Act, 2002 exempts from any limitation period.
type NoLimitKind string

// No-limit kinds (Q-T-031L.4). Other s.16 claims are added here as the app needs them.
const (
	// NoLimitSexualAssault is a proceeding based on a sexual assault (s.16(1)(h)).
	NoLimitSexualAssault NoLimitKind = "sexual_assault"
	// NoLimitSexualMisconduct is a proceeding based on other misconduct of a sexual nature
	// within s.16(1)(h.1) (verify the relationship conditions against the current text).
	NoLimitSexualMisconduct NoLimitKind = "sexual_misconduct"
	// NoLimitEnvironmental is an environmental claim; it has no period only while it has not
	// been discovered (s.17, verify).
	NoLimitEnvironmental NoLimitKind = "environmental"
)

// SchedulePeriod is a limitation period set by another Act and listed in the Schedule to
// the Limitations Act, 2002 (s.19). It replaces the basic and ultimate periods.
type SchedulePeriod struct {
	Provision     string // citation of the listed provision, e.g. "Act X, s. 12"
	Years         int
	Months        int
	Days          int
	FromDiscovery bool // false: runs from the act or omission
}

// Schedule looks up a Schedule period for a claim type. ok is false when none applies.
type Schedule interface {
	Lookup(ctx context.Context, claimType string) (p SchedulePeriod, ok bool, err error)
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
