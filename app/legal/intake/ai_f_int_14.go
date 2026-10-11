package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
)

// ActionGenerateEngagementLetter is the consult-outcome action that runs F-INT-14.
const ActionGenerateEngagementLetter = "consult-outcome > Generate engagement letter draft"

//go:embed prompts/f-int-14.md
var promptFINT14 embed.FS

// EngagementLetterDraftDef is F-INT-14: on the consult-outcome screen, once the consult ends
// with the firm taking the matter, the agent looks the client and matter up, drafts the
// engagement letter with draft_document and hands it to the engagement area by opening gate
// docs.engagement_letter (G5) with submit_for_approval. Nothing goes to the client until a
// lawyer approves. A screen action starts it through the screen-action route, acting for the
// user who asked.
func EngagementLetterDraftDef() feature.Def {
	return feature.Def{
		ID:      "F-INT-14",
		Area:    "intake",
		Name:    "Generate engagement letter draft",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionGenerateEngagementLetter,
		Actions: []string{ActionGenerateEngagementLetter},
		Tools: []string{
			"search_contacts", "search_matters", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{legaldocs.GateEngagementLetter},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.edit", "agent.ask"},
		Prompt:     promptFINT14,
		PromptPath: "prompts/f-int-14.md",
		Fixture:    "testdata/ai/f-int-14/generate-engagement-letter-draft",
	}
}
