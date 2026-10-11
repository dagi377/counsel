package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionSuggestCriteria is the ai-receptionist screen action that runs F-INT-19.
const ActionSuggestCriteria = "ai-receptionist > Suggest criteria from firm website"

//go:embed prompts/f-int-19.md
var promptFINT19 embed.FS

// ReceptionistCriteriaDef is F-INT-19: on request from the ai-receptionist screen, the agent
// drafts the receptionist's intake criteria, practice profiles and FAQ from the firm's
// website text with draft_document and opens gate platform.skill_autonomy, a tool-gap kind,
// with submit_for_approval (G-form). The receptionist uses them only once a lawyer other than
// the acting user approves.
func ReceptionistCriteriaDef() feature.Def {
	return feature.Def{
		ID:      "F-INT-19",
		Area:    "intake",
		Name:    "Suggest criteria from firm website",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionSuggestCriteria,
		Actions: []string{ActionSuggestCriteria},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"search_contacts", "search_matters", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{"platform.skill_autonomy"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.edit", "agent.ask"},
		Prompt:     promptFINT19,
		PromptPath: "prompts/f-int-19.md",
		Fixture:    "testdata/ai/f-int-19/suggest-criteria-from-firm-website",
	}
}
