package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionDraftSequence is the intake-automations screen action that runs F-INT-17.
const ActionDraftSequence = "intake-automations > Draft sequence from description"

//go:embed prompts/f-int-17.md
var promptFINT17 embed.FS

// DraftSequenceDef is F-INT-17: on request from the intake-automations screen, the agent
// drafts an intake follow-up sequence from a staff member's description with draft_document
// and opens gate platform.skill_autonomy, a tool-gap kind, with submit_for_approval. The
// sequence runs only once a person other than the acting user approves it.
func DraftSequenceDef() feature.Def {
	return feature.Def{
		ID:      "F-INT-17",
		Area:    "intake",
		Name:    "Draft sequence from description",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionDraftSequence,
		Actions: []string{ActionDraftSequence},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"search_contacts", "search_matters", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{"platform.skill_autonomy"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.edit", "agent.ask"},
		Prompt:     promptFINT17,
		PromptPath: "prompts/f-int-17.md",
		Fixture:    "testdata/ai/f-int-17/draft-sequence-from-description",
	}
}
