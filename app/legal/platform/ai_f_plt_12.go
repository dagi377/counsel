package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/kernel/ai"
)

// GateResearchMemo is G-RES: a research memo and its citation check wait on a lawyer with
// documents.release before the memo is relied on (a2 gate kind platform.research_memo).
const GateResearchMemo = "platform.research_memo"

// ActionCheckCitations is the research-workspace action that runs F-PLT-12 on request.
const ActionCheckCitations = "research-workspace > Check citations"

//go:embed prompts/f-plt-12.md
var promptFPLT12 embed.FS

// CitationCheckerDef is F-PLT-12: when a research memo is drafted, or on request from the
// research workspace, the agent reads the memo and the sources it cites, checks that every
// citation points at a passage that says what the memo says, drafts the citation report on
// the memo's matter with draft_document and opens gate platform.research_memo (G-RES) with
// submit_for_approval. A lawyer decides; the run releases nothing.
func CitationCheckerDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-12",
		Area:    "platform",
		Name:    "Citation checker",
		Source:  feature.SourceJob,
		Trigger: "event (memo drafted)",
		Actions: []string{ActionCheckCitations},
		Kind:    ai.KindExtraction,
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolDraftDocument,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{GateResearchMemo},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView, "documents.edit", agent.CapAsk},
		Prompt:     promptFPLT12,
		PromptPath: "prompts/f-plt-12.md",
		Fixture:    "testdata/ai/f-plt-12/citation-checker",
	}
}

// CitationCheckerBinding binds F-PLT-12 to a drafted memo. a2 names no memo event, so it
// listens to docs.EventDraftCreated (a new draft enters review) and the run checks only a
// research memo. An event run acts for the event's user.
func CitationCheckerBinding() (trigger.Binding, error) {
	return trigger.Bind(CitationCheckerDef(), "", legaldocs.EventDraftCreated)
}
