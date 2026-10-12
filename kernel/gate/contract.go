// CONTRACT: kernel/gate. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-007 subtasks (kind.go, open.go, jcs.go,
// version.go, decide.go, quorum.go, expiry.go, effect.go, selfapproval.go).
// Source: spec/eng/k1-kernel.html (kernel/gate), migrations/0001_init.sql (gate schema).

package gate

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

// GateID identifies one gate (one approval request version).
type GateID string

// Subject is an opaque reference to the business object a gate is about. The gate engine
// never interprets Kind or ID.
type Subject = registry.Subject

// PrincipalKind says whether a principal is a person, an agent run or a portal guest.
type PrincipalKind = registry.PrincipalKind

// Principal kinds. Only PrincipalUser may approve.
const (
	PrincipalUser  = registry.PrincipalUser
	PrincipalAgent = registry.PrincipalAgent
	PrincipalGuest = registry.PrincipalGuest
)

// Principal is who acts on a gate. An agent principal only proposes; its ActingFor user
// counts as the preparer.
type Principal = registry.Principal

// Risk is the cost of a gate's effect if it is wrong.
type Risk = registry.Risk

// Risk levels. RiskIrreversible needs MinApprovers >= 2.
const (
	RiskLow          = registry.RiskLow
	RiskMedium       = registry.RiskMedium
	RiskHigh         = registry.RiskHigh
	RiskIrreversible = registry.RiskIrreversible
)

// Status is the state of a gate.
type Status string

// Gate states. StatusApprovedCooling means the gate has its approvals but its effect waits
// for the kind's cooling-off period.
const (
	StatusPending          Status = "pending"
	StatusApprovedCooling  Status = "approved_cooling"
	StatusApproved         Status = "approved"
	StatusRejected         Status = "rejected"
	StatusChangesRequested Status = "changes_requested"
	StatusWithdrawn        Status = "withdrawn"
	StatusExpired          Status = "expired"
	StatusSuperseded       Status = "superseded"
)

// Verdict is what an approver decides.
type Verdict string

// Verdicts. Reject and changes_requested need a Reason.
const (
	VerdictApprove          Verdict = "approve"
	VerdictReject           Verdict = "reject"
	VerdictChangesRequested Verdict = "changes_requested"
)

// Decision is one approver's answer to one gate version.
type Decision struct {
	Verdict Verdict
	Reason  string
	// PayloadHash is the hash the approver saw; it must equal the gate's current hash.
	PayloadHash string
	// MFAVerifiedAt is when the approver last passed step-up auth. Every decision needs it.
	MFAVerifiedAt time.Time
	// SelfApprovalReason is the declared reason when the preparer approves under a
	// SelfApprovalPolicy exception.
	SelfApprovalReason string
}

// Approved is a gate that has all the approvals it needs, as passed to its effect handler.
type Approved = registry.Approved

// Rejected is a just-rejected gate as GateKind.OnReject sees it.
type Rejected = registry.Rejected

// EffectFunc applies an approved gate. It runs at most once per gate, inside tx, while the
// kernel holds the gate lock.
type EffectFunc = registry.EffectFunc

// GateKind declares one kind of approval gate. Slug is module-prefixed ("<module>.<slug>").
type GateKind = registry.GateKind

// Gate is the stored state of one gate version.
type Gate struct {
	ID           GateID
	Kind         string
	Version      int
	Supersedes   GateID // previous version, empty for version 1
	Subject      Subject
	Payload      json.RawMessage
	PayloadHash  string // "sha256:" + hex(SHA-256(JCS(payload)))
	Preparer     Principal
	Approvers    []Principal
	Status       Status
	SelfApproved bool
	CreatedAt    time.Time
	ExpiresAt    time.Time
	EffectiveAt  time.Time // when the effect may run; zero until approved
	EffectRan    bool
}

// SelfApprovalPolicy decides whether the preparer may approve their own gate. With no
// policy set, self-approval is always denied. An agent never approves, whatever the policy.
type SelfApprovalPolicy func(ctx context.Context, g Gate, by Principal) (allowed bool, reason string)

// Authorizer reports whether a principal holds a capability.
type Authorizer interface {
	Can(ctx context.Context, p Principal, capability string) (bool, error)
}

// CoreRegistrar is implemented by an engine that accepts kernel-owned core.* kinds. Engine
// stays unchanged (frozen contract); kernel boot type-asserts it when it registers the core
// kinds an app supplied effects for (KERNEL-CORE, additive).
type CoreRegistrar interface {
	RegisterCore(k GateKind) error
}

// Engine is the gate engine. Every method that writes takes the caller's tx.
type Engine interface {
	// Register adds a gate kind. Duplicate slugs and invalid kinds fail.
	Register(k GateKind) error
	// SetSelfApprovalPolicy installs the one optional self-approval policy.
	SetSelfApprovalPolicy(p SelfApprovalPolicy)
	// Open creates version 1 of a gate for kind on subject, prepared by preparer.
	Open(ctx context.Context, tx pgx.Tx, kind string, subject Subject, payload json.RawMessage, preparer Principal) (GateID, error)
	// Revise supersedes a pending gate with a new payload version and returns the new id.
	Revise(ctx context.Context, tx pgx.Tx, id GateID, payload json.RawMessage, by Principal) (GateID, error)
	// Decide records one decision on gate id at version. When the gate reaches its
	// approvals and no cooling-off applies, the effect runs in the same call.
	Decide(ctx context.Context, tx pgx.Tx, id GateID, version int, d Decision, by Principal) error
	// Withdraw closes a pending gate on the preparer's request.
	Withdraw(ctx context.Context, tx pgx.Tx, id GateID, by Principal) error
	// RunDue runs effects whose cooling-off has ended and expires pending gates past their
	// TTL. It returns how many effects ran.
	RunDue(ctx context.Context, tx pgx.Tx) (int, error)
	// Get returns one gate.
	Get(ctx context.Context, id GateID) (Gate, error)
}

// Sentinel errors.
var (
	ErrNotFound       = errors.New("gate: not found")
	ErrUnknownKind    = errors.New("gate: unknown kind")
	ErrDuplicateKind  = errors.New("gate: duplicate kind")
	ErrInvalidKind    = errors.New("gate: invalid kind")
	ErrInvalidPayload = errors.New("gate: invalid payload")
	ErrNotPending     = errors.New("gate: not pending")
	ErrStaleVersion   = errors.New("gate: stale version")
	ErrPayloadChanged = errors.New("gate: payload changed")
	ErrExpired        = errors.New("gate: expired")
	ErrSelfApproval   = errors.New("gate: preparer may not approve")
	ErrAgentApprover  = errors.New("gate: agent may not approve")
	ErrNotAuthorized  = errors.New("gate: approver lacks capability")
	ErrAlreadyDecided = errors.New("gate: principal already decided")
	ErrMFARequired    = errors.New("gate: step-up auth required")
	ErrReasonRequired = errors.New("gate: reason required")
	ErrNotPreparer    = errors.New("gate: only the preparer may do this")
	ErrEffectFailed   = errors.New("gate: effect failed")
)
