package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventConflictCleared is the kernel event the conflicts module publishes when a check is
// cleared (spec gate G2). It is one of the two events that start F-DOC-02.
const EventConflictCleared = "conflict.cleared"

// EventLeadConverted is the kernel event intake publishes when a lead is accepted and becomes
// a matter (spec gate G1). It is the other event that starts F-DOC-02.
const EventLeadConverted = "lead.converted"

// ActionDraftEngagementLetter is the engagement-letter-composer action that runs F-DOC-02 on
// request.
const ActionDraftEngagementLetter = "engagement-letter-composer > Draft letter"

// GateEngagementLetter is the gate kind of spec gate G5: a lawyer approves the engagement
// letter before it goes to the client.
const GateEngagementLetter = "docs.engagement_letter"

//go:embed prompts/f-doc-02.md
var promptFDOC02 embed.FS

// EngagementLetterDef is F-DOC-02: once the lead is accepted and the conflict check cleared,
// the agent reads the matter and its documents, drafts the engagement letter with
// draft_document and opens gate docs.engagement_letter (G5) with submit_for_approval. Nothing
// goes to the client until a lawyer approves.
func EngagementLetterDef() feature.Def {
	return feature.Def{
		ID:      "F-DOC-02",
		Area:    "documents",
		Name:    "draft-engagement-letter",
		Source:  feature.SourceJob,
		Trigger: "event: conflict cleared (G2) and lead accepted (G1)",
		Actions: []string{ActionDraftEngagementLetter},
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateEngagementLetter},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFDOC02,
		PromptPath: "prompts/f-doc-02.md",
		Fixture:    "testdata/ai/f-doc-02/draft-engagement-letter",
	}
}

// EngagementLetterBinding binds F-DOC-02 to the conflict-cleared and lead-converted events.
// Event runs act for the event's user, so it needs no scheduled acting user. The prompt has
// the agent draft only once the matter shows both steps done.
func EngagementLetterBinding() (trigger.Binding, error) {
	return trigger.Bind(EngagementLetterDef(), "", EventConflictCleared, EventLeadConverted)
}
