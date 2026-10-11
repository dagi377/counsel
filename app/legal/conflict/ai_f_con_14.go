package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionShowKYCSummary is the client-identification screen action that runs F-CON-14.
const ActionShowKYCSummary = "client-identification > Show KYC summary on matter"

//go:embed prompts/f-con-14.md
var promptFCON14 embed.FS

// KYCSummaryDef is F-CON-14: on the client-identification screen the agent summarizes the selected
// matter's client identification: status, check dates and risk rating. It is read-only: no write
// tool, no gate. The answer goes back through the screen-action route only and is never stored
// (AI-R-005).
func KYCSummaryDef() feature.Def {
	return feature.Def{
		ID:         "F-CON-14",
		Area:       "conflicts",
		Name:       "Show KYC summary on matter",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionShowKYCSummary,
		Actions:    []string{ActionShowKYCSummary},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), readOnlyTools...),
		Grant:      append([]string(nil), readOnlyGrant...),
		Prompt:     promptFCON14,
		PromptPath: "prompts/f-con-14.md",
		Fixture:    "testdata/ai/f-con-14/show-kyc-summary-on-matter",
	}
}
