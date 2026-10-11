package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionRequestDocuments is the client-sharing screen action that runs F-DOC-19.
const ActionRequestDocuments = "client-sharing > Request documents from client"

// GatePortalMessage is the gate kind a message to the client through the portal opens: a
// person approves it before the client sees it.
const GatePortalMessage = "platform.portal_message"

//go:embed prompts/f-doc-19.md
var promptFDOC19 embed.FS

// RequestDocumentsDef is F-DOC-19: on the client-sharing screen the agent reads the matter and
// what is already on file, works out which documents are still missing (ID, lease, pay stubs
// and the like), drafts the request to the client with draft_email and opens gate
// platform.portal_message with submit_for_approval. Nothing reaches the client until a person
// other than the acting user approves. A screen action starts it, acting for the user who
// asked, so it binds to no event or schedule.
func RequestDocumentsDef() feature.Def {
	return feature.Def{
		ID:      "F-DOC-19",
		Area:    "documents",
		Name:    "Request documents from client",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionRequestDocuments,
		Actions: []string{ActionRequestDocuments},
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents", "draft_email",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GatePortalMessage},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFDOC19,
		PromptPath: "prompts/f-doc-19.md",
		Fixture:    "testdata/ai/f-doc-19/request-documents-from-client",
	}
}
