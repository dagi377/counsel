package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventDraftCreated is the kernel event the docs module publishes when a draft is created;
// a new draft enters the review queue. It starts F-DOC-04.
const EventDraftCreated = "document.draft_created"

// ActionRunPrechecks is the document-review action that runs F-DOC-04 on request.
const ActionRunPrechecks = "document-review > Run pre-checks"

//go:embed prompts/f-doc-04.md
var promptFDOC04 embed.FS

// PrecheckOutgoingDef is F-DOC-04: when a document enters the review queue, the agent checks
// it against the matter and its sources, drafts a pre-check report with a risk level through
// draft_document, and opens gate docs.outgoing (G8) on the report with submit_for_approval.
// The reviewing lawyer decides; the run neither sends nor clears the document.
func PrecheckOutgoingDef() feature.Def {
	return feature.Def{
		ID:      "F-DOC-04",
		Area:    "documents",
		Name:    "precheck-outgoing",
		Source:  feature.SourceJob,
		Trigger: "event: document enters the review queue",
		Actions: []string{ActionRunPrechecks},
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateOutgoing},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFDOC04,
		PromptPath: "prompts/f-doc-04.md",
		Fixture:    "testdata/ai/f-doc-04/precheck-outgoing",
	}
}

// PrecheckOutgoingBinding binds F-DOC-04 to draft creation. Event runs act for the event's
// user.
func PrecheckOutgoingBinding() (trigger.Binding, error) {
	return trigger.Bind(PrecheckOutgoingDef(), "", EventDraftCreated)
}
