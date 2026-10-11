package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionDetermineObligation is the client-identification screen action that runs F-CON-13.
const ActionDetermineObligation = "client-identification > Determine obligation"

//go:embed prompts/f-con-13.md
var promptFCON13 embed.FS

// DetermineObligationDef is F-CON-13: on the client-identification screen the agent proposes
// whether the firm must identify or verify the selected client under the law society rules, with
// the reason. It is read-only: no write tool, no gate. The answer goes back through the screen-
// action route only and is never stored (AI-R-005).
func DetermineObligationDef() feature.Def {
	return feature.Def{
		ID:         "F-CON-13",
		Area:       "conflicts",
		Name:       "Determine obligation",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionDetermineObligation,
		Actions:    []string{ActionDetermineObligation},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), readOnlyTools...),
		Grant:      append([]string(nil), readOnlyGrant...),
		Prompt:     promptFCON13,
		PromptPath: "prompts/f-con-13.md",
		Fixture:    "testdata/ai/f-con-13/determine-obligation",
	}
}
