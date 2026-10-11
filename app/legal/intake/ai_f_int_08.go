package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventConsultRetained is the kernel event the consult module publishes when gate G3
// (intake.consult_outcome) is approved with outcome retain. It starts F-INT-08.
const EventConsultRetained = "consultation.retained"

// ActionPrefillMatter is the lead-conversion screen action that runs F-INT-08 on request.
const ActionPrefillMatter = "lead-conversion > Prefill matter"

// GateOpenMatter is the gate kind of spec gate G-open: a lawyer approves the prefilled matter
// before it is opened.
const GateOpenMatter = "intake.open_matter"

//go:embed prompts/f-int-08.md
var promptFINT08 embed.FS

// ConversionPrefillDef is F-INT-08: once a lead is retained, the agent prefills the new matter
// with draft_document and opens gate intake.open_matter (G-open), a tool-gap kind, with
// submit_for_approval. No matter opens until a lawyer approves.
func ConversionPrefillDef() feature.Def {
	return feature.Def{
		ID:      "F-INT-08",
		Area:    "intake",
		Name:    "Conversion prefill",
		Source:  feature.SourceJob,
		Trigger: "event: G3 retained",
		Actions: []string{ActionPrefillMatter},
		Tools: []string{
			"search_contacts", "search_matters", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateOpenMatter},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.edit", "agent.ask"},
		Prompt:     promptFINT08,
		PromptPath: "prompts/f-int-08.md",
		Fixture:    "testdata/ai/f-int-08/conversion-prefill",
	}
}

// ConversionPrefillBinding binds F-INT-08 to the retained-consult event. Event runs act for
// the event's user, so it needs no scheduled acting user.
func ConversionPrefillBinding() (trigger.Binding, error) {
	return trigger.Bind(ConversionPrefillDef(), "", EventConsultRetained)
}
