package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventRevisionRequested is the docs event a user's revise request on an existing document
// publishes. It starts F-DOC-11.
const EventRevisionRequested = "document.revision_requested"

//go:embed prompts/f-doc-11.md
var promptFDOC11 embed.FS

// TrackedRevisionDef is F-DOC-11: on a revise request, the agent reads the document at its
// current version and the request, drafts the revision as a redline (each change shown
// against the current text) with draft_document and opens gate docs.outgoing (G8) with
// submit_for_approval. The current version stays as it is until a lawyer approves.
func TrackedRevisionDef() feature.Def {
	return feature.Def{
		ID:      "F-DOC-11",
		Area:    "documents",
		Name:    "tracked-revision",
		Source:  feature.SourceJob,
		Trigger: "event: revise request on an existing document",
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateOutgoing},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFDOC11,
		PromptPath: "prompts/f-doc-11.md",
		Fixture:    "testdata/ai/f-doc-11/tracked-revision",
	}
}

// TrackedRevisionBinding binds F-DOC-11 to the revise-request event. Event runs act for the
// event's user, so it needs no scheduled acting user.
func TrackedRevisionBinding() (trigger.Binding, error) {
	return trigger.Bind(TrackedRevisionDef(), "", EventRevisionRequested)
}
