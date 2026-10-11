package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventWorkflowDraftSaved is the kernel event work publishes when a workflow draft is saved
// in the workflow editor. It starts F-WRK-06.
const EventWorkflowDraftSaved = "work.workflow_draft_saved"

// ActionLintWorkflow is the workflow-editor screen action that runs F-WRK-06 on request.
const ActionLintWorkflow = "workflow-editor > Lint workflow"

// GateWorkflowPublish is the gate kind of spec gate G-W3: a person with work.workflows
// publishes a workflow version.
const GateWorkflowPublish = "work.workflow_publish"

//go:embed prompts/f-wrk-06.md
var promptFWRK06 embed.FS

// WorkflowLintDef is F-WRK-06: when a workflow draft is saved, the agent reads the workflow's
// matter, work items and documents, drafts a lint report with draft_document and opens gate
// work.workflow_publish (G-W3) on it with submit_for_approval. work.workflow_publish is a
// tool-gap kind: no tool creates it, so the report draft is its subject. The workflow is
// published only once a person with work.workflows approves.
func WorkflowLintDef() feature.Def {
	return feature.Def{
		ID:      "F-WRK-06",
		Area:    "work",
		Name:    "Workflow lint",
		Source:  feature.SourceJob,
		Trigger: "event: workflow draft saved",
		Actions: []string{ActionLintWorkflow},
		Tools: []string{
			"get_matter", "list_work_items", "list_documents", "read_document",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateWorkflowPublish},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "work.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFWRK06,
		PromptPath: "prompts/f-wrk-06.md",
		Fixture:    "testdata/ai/f-wrk-06/workflow-lint",
	}
}

// WorkflowLintBinding binds F-WRK-06 to a saved workflow draft. Event runs act for the
// event's user, so it needs no scheduled acting user.
func WorkflowLintBinding() (trigger.Binding, error) {
	return trigger.Bind(WorkflowLintDef(), "", EventWorkflowDraftSaved)
}
