package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventWorkItemCreated is the kernel event the work module publishes when a task is created,
// including a task from the matter plan. It starts F-DOC-03.
const EventWorkItemCreated = "work_item.created"

// ActionDraftWithCitations is the ai-draft-workspace action that runs F-DOC-03 on a user's
// instruction.
const ActionDraftWithCitations = "ai-draft-workspace > Draft"

// GateOutgoing is the gate kind of spec gate G8: a lawyer approves an outgoing document
// before it leaves the firm.
const GateOutgoing = "docs.outgoing"

//go:embed prompts/f-doc-03.md
var promptFDOC03 embed.FS

// DraftWithCitationsDef is F-DOC-03: on a user's instruction or a task from the matter plan,
// the agent reads the matter's documents, drafts with a citation for every fact it takes from
// them through draft_document, and opens gate docs.outgoing (G8) with submit_for_approval.
// Nothing leaves the firm until a lawyer approves.
func DraftWithCitationsDef() feature.Def {
	return feature.Def{
		ID:      "F-DOC-03",
		Area:    "documents",
		Name:    "draft-with-citations",
		Source:  feature.SourceJob,
		Trigger: "event: user instruction or task from the matter plan",
		Actions: []string{ActionDraftWithCitations},
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateOutgoing},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFDOC03,
		PromptPath: "prompts/f-doc-03.md",
		Fixture:    "testdata/ai/f-doc-03/draft-with-citations",
	}
}

// DraftWithCitationsBinding binds F-DOC-03 to task creation; a user's instruction reaches it
// through the ai-draft-workspace action. Event runs act for the event's user.
func DraftWithCitationsBinding() (trigger.Binding, error) {
	return trigger.Bind(DraftWithCitationsDef(), "", EventWorkItemCreated)
}
