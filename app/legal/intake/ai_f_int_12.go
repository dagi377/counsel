package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionDraftFormFromDescription is the intake-form-builder action that runs F-INT-12.
const ActionDraftFormFromDescription = "intake-form-builder > Draft form from description"

// GatePublicWording is the gate kind a person approves before wording the agent drafted is
// shown to the public, here an intake form's fields and questions.
const GatePublicWording = "intake.public_wording"

//go:embed prompts/f-int-12.md
var promptFINT12 embed.FS

// DraftFormFromDescriptionDef is F-INT-12: on the intake-form-builder screen, from a short
// description ("a form for slip-and-fall cases"), the agent proposes the form's fields and
// questions with draft_document and opens gate intake.public_wording, a tool-gap kind, with
// submit_for_approval. Nothing is published until a person approves the wording. A screen
// action starts it through the screen-action route, acting for the user who asked.
func DraftFormFromDescriptionDef() feature.Def {
	return feature.Def{
		ID:      "F-INT-12",
		Area:    "intake",
		Name:    "Draft form from description",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionDraftFormFromDescription,
		Actions: []string{ActionDraftFormFromDescription},
		Tools: []string{
			"search_contacts", "search_matters", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GatePublicWording},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.edit", "agent.ask"},
		Prompt:     promptFINT12,
		PromptPath: "prompts/f-int-12.md",
		Fixture:    "testdata/ai/f-int-12/draft-form-from-description",
	}
}
