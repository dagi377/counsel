package deadlines

import (
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// Kernel event types that start F-DL-02 (spec/eng/a2-interfaces.json events). An intake is
// an intake-module lead, so "intake created" is lead.created; claim facts live on the matter,
// so "claim facts updated" is matter.updated (its payload names the changed fields only).
const (
	EventIntakeCreated     = "lead.created"
	EventClaimFactsUpdated = "matter.updated"
)

// GateCalSecondCheck is G7b: a second lawyer checks a limitation warning before it goes to a
// non-client. It is a tool_gap kind: no write tool opens it directly.
const GateCalSecondCheck = "cal.second_check"

// SubjectDraft is the gate subject type of a draft (a2 document.draft_created).
const SubjectDraft = "draft"

// LimitationAssessmentBinding binds F-DL-02 to an intake created or its claim facts updated.
// Event runs act for the event's user. The on-request starts are the Def's two
// limitation-assessment screen actions, served by trigger.Actions.
func LimitationAssessmentBinding() (trigger.Binding, error) {
	return trigger.Bind(LimitationAssessmentDef(), "", EventIntakeCreated, EventClaimFactsUpdated)
}

// SecondCheckInput is the submit_for_approval input that sends a limitation warning the run
// drafted to G7b. The draft must be one this run created; submit_for_approval refuses any
// other subject, and any gate kind outside the Def.
func SecondCheckInput(draftID string) (json.RawMessage, error) {
	if draftID == "" {
		return nil, fmt.Errorf("second check: draft id required: %w", ErrBadProposal)
	}
	// rule: AI-R-003
	// The run only submits; a second lawyer, never the acting user or the agent, decides.
	b, err := json.Marshal(map[string]string{
		"subject_type": SubjectDraft, "subject_id": draftID, "gate_kind": GateCalSecondCheck,
	})
	if err != nil {
		return nil, fmt.Errorf("second check: %w", err)
	}
	return b, nil
}
