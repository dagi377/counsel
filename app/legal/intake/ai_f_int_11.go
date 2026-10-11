package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionGuidedChatIntake is the public-intake-form screen action that runs F-INT-11.
const ActionGuidedChatIntake = "public-intake-form > Guided chat intake"

var (
	// inthAnswerTools are the read tools of the read-only intake screen answers (F-INT-11,
	// F-INT-13, F-INT-16, F-INT-18): no write tool, so no gate.
	inthAnswerTools = []string{"search_contacts", "search_matters"}
	// inthAnswerGrant is the capability grant those reads need.
	inthAnswerGrant = []string{"contacts.view", "matters.view"}
)

//go:embed prompts/f-int-11.md
var promptFINT11 embed.FS

// GuidedChatIntakeDef is F-INT-11: on the public intake form the agent runs an optional
// conversational mode that asks the follow-up questions the form needs and proposes the
// answers for its fields. It never gives legal advice. It is read-only: no write tool, no
// gate. The answer goes back through the screen-action route only and is never stored
// (AI-R-005); the person filling the form decides what goes into the fields.
func GuidedChatIntakeDef() feature.Def {
	return feature.Def{
		ID:         "F-INT-11",
		Area:       "intake",
		Name:       "Guided chat intake",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionGuidedChatIntake,
		Actions:    []string{ActionGuidedChatIntake},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), inthAnswerTools...),
		Grant:      append([]string(nil), inthAnswerGrant...),
		Prompt:     promptFINT11,
		PromptPath: "prompts/f-int-11.md",
		Fixture:    "testdata/ai/f-int-11/guided-chat-intake",
	}
}
