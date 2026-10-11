package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionTriageSuggestions is the work-list screen action that runs F-WRK-03 on request.
const ActionTriageSuggestions = "work-list > Triage suggestions"

//go:embed prompts/f-wrk-03.md
var promptFWRK03 embed.FS

// TriageDef is F-WRK-03: when a work item is created untriaged (client portal requests
// included), the agent ranks the matter's open items and documents against it by embedding
// and suggests a category, priority and assignee, each with a citation. It is read-only: no
// write tool, no gate, and the suggestions go back to the caller only; a person accepts or
// ignores them (AI-R-005).
func TriageDef() feature.Def {
	return feature.Def{
		ID:         "F-WRK-03",
		Area:       "work",
		Name:       "Triage new items",
		Source:     feature.SourceJob,
		Trigger:    "event: item created untriaged (incl. client portal requests)",
		Actions:    []string{ActionTriageSuggestions},
		Kind:       ai.KindEmbedding,
		Tools:      []string{"get_matter", "list_work_items", "list_documents", "read_document"},
		Grant:      []string{"matters.view", "work.view", "documents.view", "agent.ask"},
		Prompt:     promptFWRK03,
		PromptPath: "prompts/f-wrk-03.md",
		Fixture:    "testdata/ai/f-wrk-03/triage-new-items",
	}
}

// TriageBinding binds F-WRK-03 to work_item.created, the a2 event of every new item; the run
// leaves an item that is already triaged alone. Event runs act for the event's user.
func TriageBinding() (trigger.Binding, error) {
	return trigger.Bind(TriageDef(), "", legaldocs.EventWorkItemCreated)
}
