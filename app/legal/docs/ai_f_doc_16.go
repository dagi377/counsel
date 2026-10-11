package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionAskClientForMissingAnswers is the document-assembly action that runs F-DOC-16 on
// request.
const ActionAskClientForMissingAnswers = "document-assembly > Ask client for missing answers"

// GatePortalMessage is the gate kind a lawyer or clerk decides before a message goes to the
// client through the portal.
const GatePortalMessage = "platform.portal_message"

//go:embed prompts/f-doc-16.md
var promptFDOC16 embed.FS

// AskClientForMissingAnswersDef is F-DOC-16: on request from document assembly, the agent
// reads the matter and its documents, works out which answers the assembly still lacks,
// drafts a questionnaire message to the client with draft_email (never submitted for sending
// itself) and opens gate platform.portal_message with submit_for_approval. A lawyer or clerk
// approves the wording before the portal link goes to the client.
func AskClientForMissingAnswersDef() feature.Def {
	return feature.Def{
		ID:      "F-DOC-16",
		Area:    "documents",
		Name:    "Ask client for missing answers",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionAskClientForMissingAnswers,
		Actions: []string{ActionAskClientForMissingAnswers},
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents", "draft_email",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GatePortalMessage},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFDOC16,
		PromptPath: "prompts/f-doc-16.md",
		Fixture:    "testdata/ai/f-doc-16/ask-client-for-missing-answers",
	}
}
