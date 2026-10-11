package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionSuggestRebalance is the screen action that runs F-WRK-12 on request.
const ActionSuggestRebalance = "workload > Suggest rebalance"

//go:embed prompts/f-wrk-12.md
var promptFWRK12 embed.FS

// SuggestRebalanceDef is F-WRK-12: on the workload screen the agent proposes which open items
// to move between people, from the items and matters it can read; a person applies any move.
// It is read-only: no write tool, no gate. The answer goes back through the screen-action
// route only and is never stored (AI-R-005).
func SuggestRebalanceDef() feature.Def {
	return feature.Def{
		ID:         "F-WRK-12",
		Area:       "work",
		Name:       "Suggest rebalance",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionSuggestRebalance,
		Actions:    []string{ActionSuggestRebalance},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), wrkdReadTools...),
		Grant:      append([]string(nil), wrkdReadGrant...),
		Prompt:     promptFWRK12,
		PromptPath: "prompts/f-wrk-12.md",
		Fixture:    "testdata/ai/f-wrk-12/suggest-rebalance",
	}
}
