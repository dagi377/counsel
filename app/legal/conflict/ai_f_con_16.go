package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// GateLateral is the gate kind of spec gate G2c: a lawyer clears a lateral hire's prior matters before the hire joins.
const GateLateral = "conflict.lateral"

// ActionRunLateralCheck is the lateral-hire-intake screen action that runs F-CON-16.
const ActionRunLateralCheck = "lateral-hire-intake > Run lateral check"

//go:embed prompts/f-con-16.md
var promptFCON16 embed.FS

// LateralCheckDef is F-CON-16: on the lateral-hire-intake screen the agent checks the
// incoming lawyer's prior matters against the firm's clients and matters, drafts the lateral
// check with draft_document and opens gate conflict.lateral (G2c), a tool-gap kind, with
// submit_for_approval. A lawyer clears the hire.
// A screen action starts it through the screen-action route, acting for the user who asked.
func LateralCheckDef() feature.Def {
	return feature.Def{
		ID:      "F-CON-16",
		Area:    "conflicts",
		Name:    "Run lateral check",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionRunLateralCheck,
		Actions: []string{ActionRunLateralCheck},
		Tools: []string{
			"search_contacts", "search_matters", "read_document", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateLateral},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFCON16,
		PromptPath: "prompts/f-con-16.md",
		Fixture:    "testdata/ai/f-con-16/run-lateral-check",
	}
}
