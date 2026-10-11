package matter

import (
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureBriefRefresh is the spec id of "Matter brief refresh".
const FeatureBriefRefresh = "F-MAT-01"

// The a2 events that start F-MAT-01: something was added to a matter. a2 names only
// document.uploaded; the email, note and calendar event types are the ones the owning
// modules publish once they emit them (docs/QUESTIONS.md Q-FEAT-MAT-01).
const (
	EventDocumentUploaded = "document.uploaded"
	EventEmailFiled       = "email.filed"
	EventNoteAdded        = "matter.note_added"
	EventCalendarAdded    = "calendar_event.added"
)

// BriefRefreshDef is F-MAT-01: when material is added to a matter, the agent reads the
// matter, its documents and its contacts, drafts the updated brief with draft_document and,
// when a key fact changed, submits the draft for G-KF (matter.key_fact). The gate's preparer
// is the acting user; the agent never decides it.
func BriefRefreshDef() feature.Def {
	return feature.Def{
		ID:      FeatureBriefRefresh,
		Area:    "matters",
		Name:    "Matter brief refresh",
		Source:  feature.SourceJob,
		Trigger: "event: document, email, note or event added to a matter",
		Actions: []string{
			"matter-overview > Refresh brief",
			"matter-overview > Propose key-fact change",
		},
		Kind: ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// matter.key_fact has no dedicated write tool: the run drafts the change and submits it.
		GateKinds:  []string{GateKeyFact},
		Grant:      []string{"agent.ask", "contacts.view", "documents.edit", "documents.view", "matters.view"},
		Prompt:     prompts,
		PromptPath: "prompts/f-mat-01.md",
		Fixture:    "testdata/ai/f-mat-01/matter-brief-refresh",
	}
}

// BriefRefreshBinding binds F-MAT-01 to the events that add material to a matter. Event runs
// act for the event's user, so it needs no scheduled acting user.
func BriefRefreshBinding() (trigger.Binding, error) {
	return trigger.Bind(BriefRefreshDef(), "", EventDocumentUploaded, EventEmailFiled, EventNoteAdded, EventCalendarAdded)
}
