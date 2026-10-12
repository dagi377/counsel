package readiness

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// EventResearchMemoDrafted is the event the research screen emits when a research memo is
// filed on a matter for checking, with that matter and the filing lawyer as user_id. It
// starts F-RDY-02.
const EventResearchMemoDrafted = "research.memo_drafted"

// ActionCheckCitations is the research screen action that runs F-RDY-02 on request.
const ActionCheckCitations = "research > Check citations"

// GateResearchMemo is the gate kind of a checked research memo awaiting a lawyer's approval.
const GateResearchMemo = "platform.research_memo"

//go:embed prompts/f-rdy-02.md
var promptFRDY02 embed.FS

// CitationCheckerDef is F-RDY-02: when a research memo is filed, or on request from the
// research screen, the agent finds each authority the memo cites among the matter's documents
// with search_documents, drafts the checked memo with each citation marked found or not found
// with draft_document, and opens gate platform.research_memo with submit_for_approval. A
// lawyer other than the acting user approves the memo; the run files and sends nothing.
func CitationCheckerDef() feature.Def {
	return feature.Def{
		ID:      "F-RDY-02",
		Area:    "readiness",
		Name:    "Citation checker",
		Source:  feature.SourceJob,
		Trigger: "event",
		Actions: []string{ActionCheckCitations},
		Kind:    ai.KindExtraction,
		Tools: []string{
			agent.ToolSearchDocuments, agent.ToolDraftDocument,
			agent.ToolReportPlan, agent.ToolReportStep, agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{GateResearchMemo},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapDocumentsView, "documents.edit", agent.CapAsk},
		Prompt:     promptFRDY02,
		PromptPath: "prompts/f-rdy-02.md",
		Fixture:    "testdata/ai/f-rdy-02/citation-checker",
	}
}

// CitationCheckerBinding binds F-RDY-02 to a filed research memo. The run acts for the lawyer
// who filed it (the event's user), so it has no schedule; the screen action covers on-request
// runs.
func CitationCheckerBinding() (trigger.Binding, error) {
	return trigger.Bind(CitationCheckerDef(), "", EventResearchMemoDrafted)
}
