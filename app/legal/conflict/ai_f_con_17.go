package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionEstimateImpact is the conflict-search-settings screen action that runs F-CON-17.
const ActionEstimateImpact = "conflict-search-settings > Estimate impact"

//go:embed prompts/f-con-17.md
var promptFCON17 embed.FS

// EstimateImpactDef is F-CON-17: on the conflict-search-settings screen the agent estimates how
// proposed search settings would change last month's conflict checks: the hits gained and lost. It
// is read-only: no write tool, no gate. The answer goes back through the screen-action route only
// and is never stored (AI-R-005).
func EstimateImpactDef() feature.Def {
	return feature.Def{
		ID:         "F-CON-17",
		Area:       "conflicts",
		Name:       "Estimate impact",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionEstimateImpact,
		Actions:    []string{ActionEstimateImpact},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), readOnlyTools...),
		Grant:      append([]string(nil), readOnlyGrant...),
		Prompt:     promptFCON17,
		PromptPath: "prompts/f-con-17.md",
		Fixture:    "testdata/ai/f-con-17/estimate-impact",
	}
}
