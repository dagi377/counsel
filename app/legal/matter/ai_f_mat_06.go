package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureOpeningPrefill is the spec id of "Matter opening pre-fill".
const FeatureOpeningPrefill = "F-MAT-06"

// EventLeadReadyToOpen is the kernel event intake publishes once a lead has both its
// conflict clearance (G2) and its signed engagement (G5). It starts F-MAT-06.
const EventLeadReadyToOpen = "intake.lead_ready_to_open"

// ActionPrefillFromIntake is the matter-open wizard action that runs F-MAT-06 on request.
const ActionPrefillFromIntake = "matter-open-wizard > Pre-fill from intake"

//go:embed prompts/f-mat-06.md
var promptFMAT06 embed.FS

// OpeningPrefillDef is F-MAT-06: once a lead is cleared and signed, the agent reads the
// intake and drafts the matter for G-MO. The draft goes to matter.opening, a tool-gap kind,
// through draft_document and submit_for_approval; the matter opens only once a person other
// than the preparer approves.
func OpeningPrefillDef() feature.Def {
	return feature.Def{
		ID:      FeatureOpeningPrefill,
		Area:    "matters",
		Name:    "Matter opening pre-fill",
		Source:  feature.SourceJob,
		Trigger: "event: G2 cleared and G5 signed on a lead",
		Actions: []string{ActionPrefillFromIntake},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateOpening},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "contacts.view", "documents.edit", "documents.view", "matters.view"},
		Prompt:     promptFMAT06,
		PromptPath: "prompts/f-mat-06.md",
		Fixture:    "testdata/ai/f-mat-06/matter-opening-pre-fill",
	}
}

// OpeningPrefillBinding binds F-MAT-06 to the lead-ready event. Event runs act for the
// event's user, so it needs no scheduled acting user.
func OpeningPrefillBinding() (trigger.Binding, error) {
	return trigger.Bind(OpeningPrefillDef(), "", EventLeadReadyToOpen)
}
