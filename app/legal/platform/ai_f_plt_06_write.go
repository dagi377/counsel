package platform

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/matter"
)

// ClosingStarterBinding binds F-PLT-06 to a matter's stage change; the run reads the matter
// and drafts nothing unless it is at its concluded stage. Event runs act for the event's
// user. "N days with no activity" has no kernel event and the trigger names no schedule; the
// weekly stale matter sweep (F-PLT-11) covers inactive matters (Q-FEAT-PLT-08).
func ClosingStarterBinding() (trigger.Binding, error) {
	return trigger.Bind(ClosingStarterDef(), "", matter.EventStageChanged)
}

// ClosingSubmitInput is the submit_for_approval input that sends a closing draft the run made
// with draft_document to its tool-gap gate: the final account to G-B2
// (billing.bill_approval) or the closing checklist to G13 (platform.closing). The trust
// refund is not one: prepare_trust_withdrawal opens G11 itself. submit_for_approval refuses a
// draft the run did not make and any kind outside the Def.
func ClosingSubmitInput(draftID, kind string) (json.RawMessage, error) {
	if draftID == "" {
		return nil, fmt.Errorf("closing submit: draft id required: %w", ErrBadClosingItem)
	}
	// rule: ARC-I-035
	if !slices.Contains([]string{bill.GateBillApproval, matter.GateClosing}, kind) {
		return nil, fmt.Errorf("closing submit: kind %q: %w", kind, ErrBadClosingItem)
	}
	// rule: AI-R-003
	// The run only submits; a person other than the acting user, never the agent, decides.
	b, err := json.Marshal(map[string]string{"subject_type": "document", "subject_id": draftID, "gate_kind": kind})
	if err != nil {
		return nil, fmt.Errorf("closing submit: %w", err)
	}
	return b, nil
}
