package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// GateAccessChange is the gate kind of spec gate G-PERM: a change to who may see or act on
// firm records takes effect only after an owner approves it.
const GateAccessChange = "platform.access_change"

// ActionAccessReview is the users-roles action that runs F-PLT-08 on request.
const ActionAccessReview = "users-roles > Access review"

//go:embed prompts/f-plt-08.md
var promptFPLT08 embed.FS

// AccessReviewDef is F-PLT-08: each quarter the agent reviews who holds access to the firm's
// matters, drafts fix proposals on the firm with draft_document (subject_ref "firm": access is
// the firm's, not one matter's) and opens gate platform.access_change (G-PERM), a tool-gap
// kind, with submit_for_approval. No access changes until an owner approves.
func AccessReviewDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-08",
		Area:    "platform",
		Name:    "Access review",
		Source:  feature.SourceJob,
		Trigger: "schedule: quarterly",
		Actions: []string{ActionAccessReview},
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolDraftDocument,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{GateAccessChange},
		// The run's agent principal holds the capabilities of its tools, and the firm-draft
		// capability draft_document checks for a draft on the firm subject (Q-FORM-SUBJECT).
		Grant: []string{
			agent.CapMattersView, agent.CapDocumentsView, "documents.edit", agent.CapAsk, agent.CapIntakeForms,
		},
		Prompt:     promptFPLT08,
		PromptPath: "prompts/f-plt-08.md",
		Fixture:    "testdata/ai/f-plt-08/access-review",
	}
}

// AccessReviewBinding binds F-PLT-08 to its quarterly schedule. A scheduled run has no event
// user, so it acts for the feature's designated lawyer in firm settings, read at each tick
// (trigger.BindDesignated); with none named the feature is unready and does not run.
func AccessReviewBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(AccessReviewDef())
}
