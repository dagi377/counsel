package matter

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureSummarizeThisWeek is the spec id of "Summarize this week".
const FeatureSummarizeThisWeek = "F-MAT-23"

// summarizethisweekTrigger is how features.json words F-MAT-23's start: a screen action only.
const summarizethisweekTrigger = "on request: firm-feed > Summarize this week"

//go:embed prompts/f-mat-23.md
var promptFMAT23 embed.FS

// SummarizeThisWeekDef is F-MAT-23: on the firm-feed screen the agent writes a narrative digest of the filtered feed for this week, each fact cited. It is read-only: no write tool, no gate, and the answer goes back
// to the screen-action caller only, never stored (AI-R-005).
func SummarizeThisWeekDef() feature.Def {
	return feature.Def{
		ID:      FeatureSummarizeThisWeek,
		Area:    "matters",
		Name:    "Summarize this week",
		Source:  feature.SourceScreenAction,
		Trigger: summarizethisweekTrigger,
		// The screen action is the trigger itself: "firm-feed > Summarize this week".
		Actions: []string{strings.TrimPrefix(summarizethisweekTrigger, "on request: ")},
		Kind:    ai.KindChat,
		Tools:   []string{"get_matter", "search_documents", "read_document", "search_contacts"},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{"contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFMAT23,
		PromptPath: "prompts/f-mat-23.md",
		Fixture:    "testdata/ai/f-mat-23/summarize-this-week",
	}
}
