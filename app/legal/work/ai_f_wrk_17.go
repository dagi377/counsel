package work

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureExplainNumber is the spec id of "Explain this number".
const FeatureExplainNumber = "F-WRK-17"

// explainNumberTrigger is how features.json words F-WRK-17's start: a screen action only.
const explainNumberTrigger = "on request: work-reports > Explain this number"

//go:embed prompts/f-wrk-17.md
var promptFWRK17 embed.FS

// ExplainNumberDef is F-WRK-17: On the work-reports screen the agent reads the work items and documents behind the selected number and explains it in a narrative. It is read-only: no write tool, no gate, and the answer
// goes back to the screen-action caller only, never stored (AI-R-005).
func ExplainNumberDef() feature.Def {
	return feature.Def{
		ID:      FeatureExplainNumber,
		Area:    "work",
		Name:    "Explain this number",
		Source:  feature.SourceScreenAction,
		Trigger: explainNumberTrigger,
		// The screen action is the trigger itself: "work-reports > Explain this number".
		Actions: []string{strings.TrimPrefix(explainNumberTrigger, "on request: ")},
		Kind:    ai.KindChat,
		Tools:   []string{"get_matter", "list_work_items", "list_documents", "read_document"},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{"documents.view", "matters.view", "work.view"},
		Prompt:     promptFWRK17,
		PromptPath: "prompts/f-wrk-17.md",
		Fixture:    "testdata/ai/f-wrk-17/explain-this-number",
	}
}
