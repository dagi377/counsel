package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionDraftTemplate is the workflow-templates screen action that runs F-WRK-14.
const ActionDraftTemplate = "workflow-templates > Draft template from past matters"

//go:embed prompts/f-wrk-14.md
var promptFWRK14 embed.FS

// DraftTemplateDef is F-WRK-14: on request from workflow-templates, the agent mines the
// completed matters of the selected type (their work items and documents), drafts a workflow
// template from the steps they share with draft_document and opens gate
// work.workflow_publish (G-W3) on the draft with submit_for_approval. work.workflow_publish is
// a tool-gap kind: no tool creates a template, so the draft is the gate's subject. The
// template is published only once a person with work.workflows approves.
func DraftTemplateDef() feature.Def {
	return feature.Def{
		ID:      "F-WRK-14",
		Area:    "work",
		Name:    "Draft template from past matters",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionDraftTemplate,
		Actions: []string{ActionDraftTemplate},
		Tools: []string{
			"get_matter", "list_work_items", "list_documents", "read_document",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateWorkflowPublish},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "work.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFWRK14,
		PromptPath: "prompts/f-wrk-14.md",
		Fixture:    "testdata/ai/f-wrk-14/draft-template-from-past-matters",
	}
}
