// CONTRACT: app/legal/gates. Exported shapes here are frozen after wave 0; changes are
// additive only and need a docs/QUESTIONS.md entry. Bodies live in the T-007L subtasks.

// Package gates registers the legal gate kinds with the kernel gate engine and holds the
// legal approval policies: sole-practitioner self-approval, trust cooling-off periods and
// the human non-preparer decision check.
package gates

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrUnknownKind is returned for a gate code that is not a registered legal kind.
	ErrUnknownKind = errors.New("gates: unknown gate kind")
	// ErrSpecCodeAsGateCode is returned when a spec id such as G-B2 is used as gate_code (CF-04).
	ErrSpecCodeAsGateCode = errors.New("gates: spec gate id used as gate_code")
	// ErrPreparerApproves is returned when the preparer tries to decide their own gate.
	ErrPreparerApproves = errors.New("gates: preparer cannot approve")
	// ErrAgentApproves is returned when an agent principal tries to decide a gate.
	ErrAgentApproves = errors.New("gates: agent cannot approve")
	// ErrMissingCapability is returned when the decider lacks the kind's approver capability.
	ErrMissingCapability = errors.New("gates: decider lacks approver capability")
	// ErrCoolingOff is returned when a gate is decided before its cooling-off period ends.
	ErrCoolingOff = errors.New("gates: cooling-off period not over")
)

// Kind is one legal gate kind. Code is the kind slug (billing.bill_approval) and SpecGate
// keeps the spec id (G-B2). The legal approval policy must also apply the spec/gates.json
// fields dual_when (two distinct approvers once the firm has two eligible signatories) and
// sole_owner_delay (a sole eligible approver may decide alone after the delay, with
// notification); see docs/QUESTIONS.md Q-002.
type Kind struct {
	Code       string
	SpecGate   string
	Approver   string // capability a decider must hold
	CoolingOff time.Duration
}

// Principal is who prepares or decides a gate. An agent principal carries the user it acts
// for, who counts as a preparer.
type Principal struct {
	UserID       string
	Agent        bool
	ActingFor    string
	Capabilities []string
}

// Gate is the part of an open gate the legal policies look at.
type Gate struct {
	ID       string
	Code     string
	Preparer Principal
	OpenedAt time.Time
}

// SelfApprovalInput is what the sole-practitioner policy decides on.
type SelfApprovalInput struct {
	Gate            Gate
	By              Principal
	ActiveLicensees int
	SettingEnabled  bool // the firm's sole-practitioner self-approval setting
}

// SelfApprovalResult is the policy outcome. Exception is recorded as self_approval_exception.
type SelfApprovalResult struct {
	Allowed   bool
	Reason    string
	Exception bool
}

// Gates is the legal gate surface other tasks call.
type Gates interface {
	// Kinds lists every registered legal gate kind, ordered by Code.
	Kinds(ctx context.Context) ([]Kind, error)
	// Kind returns one kind by its slug, or ErrUnknownKind.
	Kind(ctx context.Context, code string) (Kind, error)
	// SelfApproval applies the sole-practitioner policy (default deny).
	SelfApproval(ctx context.Context, in SelfApprovalInput) (SelfApprovalResult, error)
	// CheckDecision checks that by may decide g at now: a human with the approver capability
	// who is not the preparer, after the cooling-off period.
	CheckDecision(ctx context.Context, g Gate, by Principal, now time.Time) error
}
