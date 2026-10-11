package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionNonEngagementLetter is the consult-outcome screen action that runs F-INT-15.
const ActionNonEngagementLetter = "consult-outcome > Generate non-engagement letter"

//go:embed prompts/f-int-15.md
var promptFINT15 embed.FS

// NonEngagementLetterDef is F-INT-15: on request from the consult-outcome screen, the agent
// drafts the non-engagement letter for a consultation the firm will not take on with
// draft_note, for gate intake.consult_outcome (G3). The letter states what was and was not
// done and mentions limitation risk; nothing is sent until a lawyer other than the acting
// user approves.
func NonEngagementLetterDef() feature.Def {
	return feature.Def{
		ID:      "F-INT-15",
		Area:    "intake",
		Name:    "Generate non-engagement letter",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionNonEngagementLetter,
		Actions: []string{ActionNonEngagementLetter},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"search_contacts", "search_matters", "draft_note",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{"intake.consult_outcome"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "matters.edit", "agent.ask"},
		Prompt:     promptFINT15,
		PromptPath: "prompts/f-int-15.md",
		Fixture:    "testdata/ai/f-int-15/generate-non-engagement-letter",
	}
}
