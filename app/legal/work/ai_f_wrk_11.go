package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionCreateFromNote is the work-item-create screen action that runs F-WRK-11.
const ActionCreateFromNote = "work-item-create > Create from email / call note"

//go:embed prompts/f-wrk-11.md
var promptFWRK11 embed.FS

// CreateFromNoteDef is F-WRK-11: on request from work-item-create, the agent reads the
// selected email or call note filed on the matter, extracts the commitments it finds and
// drafts one proposed task per commitment with create_task. It opens gate work.output (G-W1)
// on each draft with submit_for_approval; a draft becomes a work item only when a person with
// work.approve confirms it.
func CreateFromNoteDef() feature.Def {
	return feature.Def{
		ID:      "F-WRK-11",
		Area:    "work",
		Name:    "Create from email / call note",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionCreateFromNote,
		Actions: []string{ActionCreateFromNote},
		Kind:    ai.KindExtraction,
		Tools: []string{
			"get_matter", "list_work_items", "list_documents", "read_document",
			"create_task", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateWorkOutput},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "work.view", "documents.view", "work.edit", "agent.ask"},
		Prompt:     promptFWRK11,
		PromptPath: "prompts/f-wrk-11.md",
		Fixture:    "testdata/ai/f-wrk-11/create-from-email-call-note",
	}
}
