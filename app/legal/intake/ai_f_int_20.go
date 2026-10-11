package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionMapAnswersToLead is the intake-questionnaire-send screen action that runs F-INT-20.
const ActionMapAnswersToLead = "intake-questionnaire-send > Map answers to lead"

//go:embed prompts/f-int-20.md
var promptFINT20 embed.FS

// MapAnswersToLeadDef is F-INT-20: on request from the intake-questionnaire-send screen, the
// agent maps a returned questionnaire's answers to the lead's fields and ranks the contacts
// and matters they match. It is read-only: no write tool, no gate. The ranked rows and their
// citations go back in the action's answer only (AI-R-005); staff copy what they accept.
func MapAnswersToLeadDef() feature.Def {
	return feature.Def{
		ID:         "F-INT-20",
		Area:       "intake",
		Name:       "Map answers to lead",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionMapAnswersToLead,
		Actions:    []string{ActionMapAnswersToLead},
		Kind:       ai.KindExtraction,
		Tools:      append([]string(nil), readTools...),
		Grant:      append([]string(nil), readGrant...),
		Prompt:     promptFINT20,
		PromptPath: "prompts/f-int-20.md",
		Fixture:    "testdata/ai/f-int-20/map-answers-to-lead",
	}
}
