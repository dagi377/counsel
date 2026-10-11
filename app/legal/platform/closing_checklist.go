package platform

import (
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/kernel/agenttools"
)

// BlockerTrustBalance is the closing checklist item a matter with money in trust, or an
// overdrawn trust ledger, fails: it cannot close until the balance is zero.
const BlockerTrustBalance = "trust balance is not zero"

// ClosingChecklist is F-PLT-11's closing checklist for one matter and the platform.closing
// (G13) payload it carries. TrustBalanceCents is the signed balance as get_trust_balance
// returned it: an overdrawn ledger stays negative and is never shown as zero.
type ClosingChecklist struct {
	MatterID          string
	TrustBalanceCents int64
	// Blockers lists every item that keeps the matter from closing; empty means ready.
	Blockers []string
}

// Ready reports whether no item blocks the closing.
func (c ClosingChecklist) Ready() bool { return len(c.Blockers) == 0 }

// Payload is the platform.closing gate payload: matter_id and trust_balance_cents.
func (c ClosingChecklist) Payload() (json.RawMessage, error) {
	b, err := json.Marshal(closingPayload{MatterID: c.MatterID, TrustBalanceCents: c.TrustBalanceCents})
	if err != nil {
		return nil, fmt.Errorf("closing payload: %w", err)
	}
	return b, nil
}

type closingPayload struct {
	MatterID          string `json:"matter_id"`
	TrustBalanceCents int64  `json:"trust_balance_cents"`
}

// NewClosingChecklist fills matterID's closing checklist with trustBalanceCents, the
// available_cents a get_trust_balance read returned in run runID. The balance must be one
// the run read (pv, the run's provenance), else agenttools.ErrUnsourcedValue: the payload
// never carries an invented figure.
func NewClosingChecklist(pv *agenttools.Provenance, runID, matterID string, trustBalanceCents int64) (ClosingChecklist, error) {
	c := ClosingChecklist{MatterID: matterID, TrustBalanceCents: trustBalanceCents}
	payload, err := c.Payload()
	if err != nil {
		return ClosingChecklist{}, err
	}
	// rule: ARC-U-027
	// The trust balance on the gate is a figure get_trust_balance returned in this run.
	if err := pv.Check(runID, []string{"/trust_balance_cents"}, payload); err != nil {
		return ClosingChecklist{}, fmt.Errorf("closing checklist: trust_balance_cents: %w", err)
	}
	// rule: BIL-L-036
	// By-Law 9: a matter with money in trust cannot close (G13 refuses a close while any
	// trust ledger is not $0.00), and an overdrawn ledger is a blocker too: the signed
	// balance is kept as read, never clamped to zero, so an overdraft is never hidden.
	if trustBalanceCents != 0 {
		c.Blockers = append(c.Blockers, BlockerTrustBalance)
	}
	return c, nil
}
