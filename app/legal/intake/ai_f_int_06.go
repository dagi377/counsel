package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// Kernel events the consult module publishes after a consultation. Either starts F-INT-06.
const (
	// EventConsultationEnded: a consultation's end time passed.
	EventConsultationEnded = "consultation.ended"
	// EventDictationUploaded: the lawyer uploaded a dictation for a consultation.
	EventDictationUploaded = "consultation.dictation_uploaded"
)

// ActionDraftConsultNote is the consult-outcome screen action that runs F-INT-06 on request.
const ActionDraftConsultNote = "consult-outcome > Draft note from dictation"

// GateConsultOutcome is the gate kind of spec gate G3: a lawyer approves the consultation
// note and its outcome (retain, decline, refer) before it is recorded.
const GateConsultOutcome = "intake.consult_outcome"

//go:embed prompts/f-int-06.md
var promptFINT06 embed.FS

// ConsultNoteDef is F-INT-06: after a consultation, the agent drafts the consult note and the
// proposed outcome with draft_document and opens gate intake.consult_outcome (G3) with
// submit_for_approval (Q-FEAT-INT-06). Nothing is recorded until a lawyer approves.
func ConsultNoteDef() feature.Def {
	return feature.Def{
		ID:      "F-INT-06",
		Area:    "intake",
		Name:    "Consult note and outcome",
		Source:  feature.SourceJob,
		Trigger: "event: consultation ended or dictation uploaded",
		Actions: []string{ActionDraftConsultNote},
		Tools: []string{
			"search_contacts", "search_matters", "draft_note", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateConsultOutcome},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "matters.edit", "documents.edit", "agent.ask"},
		Prompt:     promptFINT06,
		PromptPath: "prompts/f-int-06.md",
		Fixture:    "testdata/ai/f-int-06/consult-note-and-outcome",
	}
}

// ConsultNoteBinding binds F-INT-06 to the consultation-ended and dictation-uploaded events.
// Event runs act for the event's user, so it needs no scheduled acting user.
func ConsultNoteBinding() (trigger.Binding, error) {
	return trigger.Bind(ConsultNoteDef(), "", EventConsultationEnded, EventDictationUploaded)
}
