package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
)

// ActionProposeDestructionBatch is the archive-retention action that runs F-PLT-07 on request.
const ActionProposeDestructionBatch = "archive-retention > Propose destruction batch"

//go:embed prompts/f-plt-07.md
var promptFPLT07 embed.FS

// DestructionBatchDef is F-PLT-07: each month the agent reads the closed matters' files whose
// retention period has ended, drafts the destruction batch on the firm with draft_document
// (subject_ref "firm": the batch spans matters and belongs to none) and opens gate
// platform.file_destruction (G14, legaldocs.DestructionGate), a tool-gap kind, with
// submit_for_approval. People decide; the run destroys nothing.
func DestructionBatchDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-07",
		Area:    "platform",
		Name:    "Destruction batch",
		Source:  feature.SourceJob,
		Trigger: "schedule: monthly",
		Actions: []string{ActionProposeDestructionBatch},
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolDraftDocument,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{legaldocs.DestructionGate},
		// The run's agent principal holds the capabilities of its tools, and the firm-draft
		// capability draft_document checks for a draft on the firm subject (Q-FORM-SUBJECT).
		Grant: []string{
			agent.CapMattersView, agent.CapDocumentsView, "documents.edit", agent.CapAsk, agent.CapIntakeForms,
		},
		Prompt:     promptFPLT07,
		PromptPath: "prompts/f-plt-07.md",
		Fixture:    "testdata/ai/f-plt-07/destruction-batch",
	}
}

// DestructionBatchBinding binds F-PLT-07 to its monthly schedule. A scheduled run has no event
// user, so it acts for the feature's designated lawyer in firm settings, read at each tick
// (trigger.BindDesignated); with none named the feature is unready and does not run.
func DestructionBatchBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(DestructionBatchDef())
}
