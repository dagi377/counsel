package gates

import (
	"fmt"
	"slices"
	"time"

	kaudit "github.com/dagi377/counsel/kernel/audit"
)

// TrustCoolingOff is the cooling-off period of the trust kinds below: a decision on one of
// them takes effect no earlier than this long after the gate opened.
const TrustCoolingOff = 24 * time.Hour

// coolingOffKinds are the gate codes that wait TrustCoolingOff, ordered by code.
var coolingOffKinds = []string{
	"trust.reconciliation_signoff",
	"trust.regulatory_release",
	"trust.transfer",
	"trust.withdrawal",
}

// CoolingOffFor returns the cooling-off period of a gate code: TrustCoolingOff for the
// trust withdrawal, transfer, reconciliation sign-off and regulatory release kinds, else 0.
func CoolingOffFor(code string) time.Duration {
	if _, ok := slices.BinarySearch(coolingOffKinds, code); ok {
		return TrustCoolingOff
	}
	return 0
}

// WithCoolingOff returns kinds with each kind's CoolingOff raised to CoolingOffFor its
// code. A longer period already set (from spec/gates.json cooling_off) is kept.
func WithCoolingOff(kinds []SpecKind) []SpecKind {
	out := slices.Clone(kinds)
	for i := range out {
		if d := CoolingOffFor(out[i].Code); d > out[i].CoolingOff {
			out[i].CoolingOff = d
		}
	}
	return out
}

// CoolingOffEnds is when the cooling-off period of g, a gate of kind k, is over.
func CoolingOffEnds(k Kind, g Gate) time.Time {
	return g.OpenedAt.Add(k.CoolingOff)
}

// CheckCoolingOff returns ErrCoolingOff while now is before the end of g's cooling-off
// period.
func CheckCoolingOff(k Kind, g Gate, now time.Time) error {
	if end := CoolingOffEnds(k, g); now.Before(end) {
		return fmt.Errorf("gate %s (%s) until %s: %w", g.ID, k.Code, end.Format(time.RFC3339), ErrCoolingOff)
	}
	return nil
}

// CheckCoolingOffOr is CheckCoolingOff with an optional override. A nil override, or one
// CheckCoolingOffOverride refuses, leaves the cooling-off period in force. An allowed override
// returns its result; the caller records AuditCoolingOffOverride (and AuditSolePractitioner
// when Sole) in the decision transaction.
//
// rule: ARC-I-021
func CheckCoolingOffOr(k Kind, g Gate, now time.Time, o *Override) (SoDResult, error) {
	err := CheckCoolingOff(k, g, now)
	if err == nil || o == nil {
		return SoDResult{}, err
	}
	o.Gate, o.Now = g, now
	res, oerr := CheckCoolingOffOverride(*o)
	if oerr != nil {
		return SoDResult{}, fmt.Errorf("%w: override refused: %w", err, oerr)
	}
	return res, nil
}

// OverrideAuditEvent is the AuditCoolingOffOverride event for an allowed override of g.
func OverrideAuditEvent(g Gate, o Override) kaudit.Event {
	return kaudit.Event{
		ActorKind: "user",
		ActorID:   o.By.UserID,
		Action:    AuditCoolingOffOverride,
		Subject:   kaudit.Subject{Kind: g.Code, ID: g.ID},
		Detail:    map[string]string{"approval_id": g.ID, "reason": o.Reason},
	}
}
