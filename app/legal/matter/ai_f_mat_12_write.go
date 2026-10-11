package matter

import (
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// Screen actions that run F-MAT-12 on request.
const (
	ActionBuildChronology   = "matter-chronology > Build chronology"
	ActionRefreshChronology = "matter-chronology > Refresh with new material"
)

// EventDocumentFiled is the kernel event of a document filed to a matter
// (spec/eng/a2-interfaces.json document.uploaded). A filed email arrives as a document, so
// this one event covers "new document or email"; the run reads the matter and proposes
// nothing when the matter has no chronology.
const EventDocumentFiled = "document.uploaded"

// ChronologyBinding binds F-MAT-12 to a document filed on a matter. The run acts for the
// event's user. F-MAT-12 has no schedule, so no designated lawyer is involved; the screen
// actions (Def.Actions) run it for the user who asked.
func ChronologyBinding() (trigger.Binding, error) {
	return trigger.Bind(ChronologyDef(), "", EventDocumentFiled)
}

// KeyFactSubmitInput is the submit_for_approval input that opens G-KF (matter.key_fact) on a
// key-fact check the run drafted with draft_document: the only way the run opens the gate,
// since matter.key_fact has no write tool of its own.
func KeyFactSubmitInput(draftID string) (json.RawMessage, error) {
	if draftID == "" {
		return nil, fmt.Errorf("%s: key-fact check without a draft: %w", FeatureChronology, ErrBadItem)
	}
	// rule: AI-R-003
	// The run only submits; a person other than the preparer decides G-KF.
	b, err := json.Marshal(map[string]string{
		"subject_type": SubjectAgentDraft, "subject_id": draftID, "gate_kind": GateKeyFact,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", FeatureChronology, err)
	}
	return b, nil
}
