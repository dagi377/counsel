package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionProposeSlots is the consultation-booking screen action that runs F-INT-13.
const ActionProposeSlots = "consultation-booking > Propose slots"

//go:embed prompts/f-int-13.md
var promptFINT13 embed.FS

// ProposeSlotsDef is F-INT-13: on the consultation-booking screen the agent proposes
// consultation slots for the selected lead from lawyer availability, the lead's urgency and its
// limitation flag; critical leads get the earliest slots. It is read-only: no write tool, no
// gate. The answer goes back through the screen-action route only and is never stored
// (AI-R-005); a person books the slot.
func ProposeSlotsDef() feature.Def {
	return feature.Def{
		ID:         "F-INT-13",
		Area:       "intake",
		Name:       "Propose slots",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionProposeSlots,
		Actions:    []string{ActionProposeSlots},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), inthAnswerTools...),
		Grant:      append([]string(nil), inthAnswerGrant...),
		Prompt:     promptFINT13,
		PromptPath: "prompts/f-int-13.md",
		Fixture:    "testdata/ai/f-int-13/propose-slots",
	}
}
