package timekeeping

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-tim-08.md
var promptFTIM08 embed.FS

// FlagOutOfScopeDef is F-TIM-08: on a flat-fee matter's fee-arrangement screen the agent flags the
// unbilled entries that fall outside the agreed scope, each cited to its time entry, so a person
// can send a change-of-scope letter. It is read-only: no write tool, no gate, and the answer goes back
// to the screen-action caller only, never stored (AI-R-005).
func FlagOutOfScopeDef() feature.Def {
	return feature.Def{
		ID:      "F-TIM-08",
		Area:    "time",
		Name:    "Flag out-of-scope work",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: matter-fee-arrangement > Flag out-of-scope work",
		Actions: []string{"matter-fee-arrangement > Flag out-of-scope work"},
		Kind:    ai.KindChat,
		// The a2 read tools; package agent imports this one, so its constants cannot be used.
		Tools: []string{"get_matter", "list_unbilled", "list_work_items"},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{"billing.view", "matters.view", "work.view"},
		Prompt:     promptFTIM08,
		PromptPath: "prompts/f-tim-08.md",
		Fixture:    "testdata/ai/f-tim-08/flag-out-of-scope-work",
	}
}
