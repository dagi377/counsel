package gates

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	kaudit "github.com/dagi377/counsel/kernel/audit"
)

// AuditSolePractitioner is the audit action recorded whenever the firm's one active licensee
// approves work they prepared, confirms a two-approver kind alone, or overrides a cooling-off
// period alone.
const AuditSolePractitioner = "sole_practitioner"

// AuditCoolingOffOverride is the audit action recorded when a cooling-off period is overridden.
const AuditCoolingOffOverride = "cooling_off_override"

// SoleSecondConfirmDelay is the minimum time between a sole licensee's first and second
// confirmation of a kind that needs two approvers.
const SoleSecondConfirmDelay = time.Hour

var (
	// ErrSoleReason is returned when a sole-practitioner approval or an override states no reason.
	ErrSoleReason = errors.New("gates: a stated reason is required")
	// ErrSoleStepUp is returned when a sole-practitioner approval carries no step-up auth.
	ErrSoleStepUp = errors.New("gates: sole-practitioner approval needs step-up")
	// ErrSoleDelay is returned when a sole licensee's second confirmation comes too soon.
	ErrSoleDelay = errors.New("gates: second confirmation before the minimum delay")
	// ErrNotSolePractice is returned when the sole-practitioner path is asked of a firm that
	// does not have exactly one active licensee, or of a decider who is not that licensee.
	ErrNotSolePractice = errors.New("gates: firm is not a sole practice")
)

// Duty is one separation-of-duties question: may Approver approve What/ObjectID, which
// Preparer prepared? Preparer is the preparing user; for an agent preparer, the user it acted
// for. Second marks the second confirmation of a two-approver kind by a licensee who already
// gave the first at FirstAt.
type Duty struct {
	What            string
	ObjectID        string
	Preparer        string
	Approver        string
	ApproverIsAgent bool
	// Sole-practitioner evidence.
	ActiveLicensees  int
	ApproverLicensed bool // the approver is an active licensee in good standing
	StepUpAt         time.Time
	Reason           string
	Now              time.Time
	// Two-approver kinds.
	Second  bool
	FirstAt time.Time
	MinGap  time.Duration // kind's sole_owner_delay; SoleSecondConfirmDelay is the floor
}

// SoDResult is the outcome of an allowed duty. Sole is true when the sole-practitioner path
// allowed it, and the caller records AuditSolePractitioner in the same transaction.
type SoDResult struct {
	Sole bool
}

// CheckSoD is the one separation-of-duties policy every money effect, conflict clearance,
// two-approver kind and cooling-off override applies. An agent never approves. A different
// human approver passes. The preparer (or a second confirmation by the same person) passes
// only when the firm has exactly one active licensee, the approver is that licensee, the
// decision carries step-up auth and a stated reason, and, for a second confirmation, at
// least max(MinGap, SoleSecondConfirmDelay) has passed since the first with a fresh step-up.
func CheckSoD(d Duty) (SoDResult, error) {
	// rule: ARC-I-027
	if d.ApproverIsAgent {
		return SoDResult{}, fmt.Errorf("%s %s: %w", d.What, d.ObjectID, ErrAgentApproves)
	}
	if d.Approver == "" {
		return SoDResult{}, fmt.Errorf("%s %s: %w: %w", d.What, d.ObjectID, ErrPreparerApproves, errNoDecider)
	}
	if !d.Second && d.Approver != d.Preparer {
		return SoDResult{}, nil
	}
	// rule: ARC-I-021
	// Sole practice: the one active licensee may approve their own work, audited.
	if d.ActiveLicensees != 1 || !d.ApproverLicensed {
		return SoDResult{}, fmt.Errorf("%s %s: %w: %w", d.What, d.ObjectID, ErrPreparerApproves, ErrNotSolePractice)
	}
	// rule: ARC-I-017
	if d.StepUpAt.IsZero() || (!d.Now.IsZero() && d.StepUpAt.After(d.Now)) {
		return SoDResult{}, fmt.Errorf("%s %s: %w: %w", d.What, d.ObjectID, ErrPreparerApproves, ErrSoleStepUp)
	}
	if strings.TrimSpace(d.Reason) == "" {
		return SoDResult{}, fmt.Errorf("%s %s: %w: %w", d.What, d.ObjectID, ErrPreparerApproves, ErrSoleReason)
	}
	if d.Second {
		// rule: ARC-I-021
		// A two-approver kind in a sole firm: the same licensee confirms a second time, with
		// a fresh step-up, no sooner than the minimum delay after the first confirmation.
		gap := max(d.MinGap, SoleSecondConfirmDelay)
		if d.FirstAt.IsZero() || d.Now.Before(d.FirstAt.Add(gap)) {
			return SoDResult{}, fmt.Errorf("%s %s: %w", d.What, d.ObjectID, ErrSoleDelay)
		}
		if !d.StepUpAt.After(d.FirstAt) {
			return SoDResult{}, fmt.Errorf("%s %s: second confirmation: %w", d.What, d.ObjectID, ErrSoleStepUp)
		}
	}
	return SoDResult{Sole: true}, nil
}

// Override is a request to end a gate's cooling-off period early, for example for a same-day
// real-estate closing. By must be a lawyer; Reason documents why.
type Override struct {
	Gate         Gate
	By           Principal
	ByIsLawyer   bool
	SoleEvidence SoleEvidence
	Reason       string
	Now          time.Time
}

// CheckCoolingOffOverride decides an override of g's cooling-off period. A lawyer other than
// the preparer may override with a documented reason; the preparer may override only as a
// sole practitioner (CheckSoD). The caller records AuditCoolingOffOverride, and also
// AuditSolePractitioner when the result is Sole. An agent never overrides.
func CheckCoolingOffOverride(o Override) (SoDResult, error) {
	// rule: ARC-I-027
	if o.By.Agent {
		return SoDResult{}, fmt.Errorf("cooling-off override %s: %w", o.Gate.ID, ErrAgentApproves)
	}
	if !o.ByIsLawyer {
		return SoDResult{}, fmt.Errorf("cooling-off override %s needs a lawyer: %w", o.Gate.ID, ErrMissingCapability)
	}
	if strings.TrimSpace(o.Reason) == "" {
		return SoDResult{}, fmt.Errorf("cooling-off override %s: %w", o.Gate.ID, ErrSoleReason)
	}
	return CheckSoD(Duty{
		What: "cooling_off_override", ObjectID: o.Gate.ID,
		Preparer: preparerUser(o.Gate.Preparer), Approver: o.By.UserID,
		ActiveLicensees: o.SoleEvidence.ActiveLicensees, ApproverLicensed: o.SoleEvidence.ApproverLicensed,
		StepUpAt: o.SoleEvidence.StepUpAt, Reason: o.Reason, Now: o.Now,
	})
}

// preparerUser is the user a preparer principal stands for: an agent counts as its user.
func preparerUser(p Principal) string {
	if p.Agent {
		return p.ActingFor
	}
	return p.UserID
}

// SoleEvidence is what the database says about a sole-practitioner approval: the firm's
// active licensees, whether the approver is one, and the approver's decision on the gate.
type SoleEvidence struct {
	ActiveLicensees  int
	ApproverLicensed bool
	StepUpAt         time.Time
	Reason           string
}

// SoDEvidence reads SoleEvidence for userID's decision on gateID, in tx.
type SoDEvidence interface {
	SoleEvidence(ctx context.Context, tx pgx.Tx, gateID, userID string) (SoleEvidence, error)
}

// SoDApprover is the separation-of-duties check as a dependency, for a package such as the
// conflict clearance that takes it through an interface. *SoDGuard implements it.
type SoDApprover interface {
	Approve(ctx context.Context, tx pgx.Tx, gateID string, d Duty) (SoDResult, error)
}

var _ SoDApprover = (*SoDGuard)(nil)

// SoDGuard applies CheckSoD inside an effect's transaction: it reads the evidence and records
// AuditSolePractitioner when the sole path allowed the approval. A nil guard, or one missing
// its evidence or audit log, is strict: the preparer is always refused.
type SoDGuard struct {
	Evidence SoDEvidence
	Audit    kaudit.Log
	Now      func() time.Time
}

// Approve is the call every money effect and conflict clearance makes in place of comparing
// preparer and approver ids. gateID names the approval whose decision carries the step-up
// time and stated reason.
func (s *SoDGuard) Approve(ctx context.Context, tx pgx.Tx, gateID string, d Duty) (SoDResult, error) {
	if d.ApproverIsAgent || d.Approver == "" || d.Approver != d.Preparer {
		return CheckSoD(d)
	}
	if s == nil || s.Evidence == nil || s.Audit == nil {
		return SoDResult{}, fmt.Errorf("%s %s: no sole-practitioner evidence: %w", d.What, d.ObjectID, ErrPreparerApproves)
	}
	ev, err := s.Evidence.SoleEvidence(ctx, tx, gateID, d.Approver)
	if err != nil {
		return SoDResult{}, fmt.Errorf("%s %s: sole evidence: %w", d.What, d.ObjectID, err)
	}
	d.ActiveLicensees, d.ApproverLicensed, d.StepUpAt, d.Reason = ev.ActiveLicensees, ev.ApproverLicensed, ev.StepUpAt, ev.Reason
	if d.Now.IsZero() && s.Now != nil {
		d.Now = s.Now()
	}
	res, err := CheckSoD(d)
	if err != nil || !res.Sole {
		return res, err
	}
	if err := s.Audit.Append(ctx, tx, SoleAuditEvent(gateID, d)); err != nil {
		return SoDResult{}, fmt.Errorf("%s %s: audit: %w", d.What, d.ObjectID, err)
	}
	return res, nil
}

// SoleAuditEvent is the AuditSolePractitioner event for an allowed sole-practitioner duty.
func SoleAuditEvent(gateID string, d Duty) kaudit.Event {
	return kaudit.Event{
		ActorKind: "user",
		ActorID:   d.Approver,
		Action:    AuditSolePractitioner,
		Subject:   kaudit.Subject{Kind: d.What, ID: d.ObjectID},
		Detail:    map[string]string{"approval_id": gateID, "reason": d.Reason},
	}
}

// PGSoDEvidence reads SoleEvidence from core.app_user and gate.gate_decision.
type PGSoDEvidence struct{}

var _ SoDEvidence = PGSoDEvidence{}

// soleEvidenceSQL counts the firm's active licensees (the same test as the trust
// reconciliation sign-off) and reads userID's approval of the gate.
const soleEvidenceSQL = `SELECT count(*), COALESCE(bool_or(u.id = $2::uuid), false),
  (SELECT d.mfa_verified_at FROM gate.gate_decision d WHERE d.approval_id = $1::uuid AND d.decided_by = $2::uuid AND d.decision = 'approve'),
  COALESCE((SELECT d.reason FROM gate.gate_decision d WHERE d.approval_id = $1::uuid AND d.decided_by = $2::uuid AND d.decision = 'approve'), '')
FROM core.app_user u
WHERE u.principal_kind = 'human' AND u.licence_type IN ('lawyer', 'paralegal')
  AND u.licence_status = 'good_standing'
  AND u.status = 'active' AND u.offboarded_at IS NULL AND u.archived_at IS NULL
  AND (u.expires_on IS NULL OR u.expires_on >= (now() AT TIME ZONE 'America/Toronto')::date)`

// SoleEvidence implements SoDEvidence.
func (PGSoDEvidence) SoleEvidence(ctx context.Context, tx pgx.Tx, gateID, userID string) (SoleEvidence, error) {
	var (
		ev  SoleEvidence
		n   int64
		mfa *time.Time
	)
	if err := tx.QueryRow(ctx, soleEvidenceSQL, gateID, userID).Scan(&n, &ev.ApproverLicensed, &mfa, &ev.Reason); err != nil {
		return SoleEvidence{}, fmt.Errorf("sole evidence: %w", err)
	}
	ev.ActiveLicensees = int(n)
	if mfa != nil {
		ev.StepUpAt = *mfa
	}
	return ev, nil
}
