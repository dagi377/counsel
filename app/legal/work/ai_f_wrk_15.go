package work

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureExplainFailedRun is the spec id of "Explain failed or skipped run".
const FeatureExplainFailedRun = "F-WRK-15"

// explainFailedRunTrigger is how features.json words F-WRK-15's start: a screen action only.
const explainFailedRunTrigger = "on request: work-automations > Explain failed or skipped run"

//go:embed prompts/f-wrk-15.md
var promptFWRK15 embed.FS

// ExplainFailedRunDef is F-WRK-15: On the work-automations screen the agent reads the selected automation run's matter, work items and documents and says in one line why the run failed or was skipped. It is read-only: no write tool, no gate, and the answer
// goes back to the screen-action caller only, never stored (AI-R-005).
func ExplainFailedRunDef() feature.Def {
	return feature.Def{
		ID:      FeatureExplainFailedRun,
		Area:    "work",
		Name:    "Explain failed or skipped run",
		Source:  feature.SourceScreenAction,
		Trigger: explainFailedRunTrigger,
		// The screen action is the trigger itself: "work-automations > Explain failed or skipped run".
		Actions: []string{strings.TrimPrefix(explainFailedRunTrigger, "on request: ")},
		Kind:    ai.KindChat,
		Tools:   []string{"get_matter", "list_work_items", "list_documents", "read_document"},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{"documents.view", "matters.view", "work.view"},
		Prompt:     promptFWRK15,
		PromptPath: "prompts/f-wrk-15.md",
		Fixture:    "testdata/ai/f-wrk-15/explain-failed-or-skipped-run",
	}
}
