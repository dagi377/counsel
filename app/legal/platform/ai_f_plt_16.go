package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
)

// ActionRequestPortalDocuments is the client-portal screen action that runs F-PLT-16.
const ActionRequestPortalDocuments = "client-portal > Request documents"

//go:embed prompts/f-plt-16.md
var promptFPLT16 embed.FS

// RequestPortalDocumentsDef is F-PLT-16: on the client portal a lawyer asks the agent to
// request documents from the client of the selected matter. The agent works through the
// checklist (ID, source of funds, records) against what is already on file, drafts the
// portal request with a due date and a polite reminder using draft_email, and opens gate
// platform.portal_message with submit_for_approval. Nothing reaches the client until a person
// other than the acting user approves. A screen action starts it, acting for the user who
// asked, so it binds to no event or schedule.
func RequestPortalDocumentsDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-16",
		Area:    "platform",
		Name:    "Request documents",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionRequestPortalDocuments,
		Actions: []string{ActionRequestPortalDocuments},
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolDraftEmail,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{legaldocs.GatePortalMessage},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView, "documents.edit", agent.CapAsk},
		Prompt:     promptFPLT16,
		PromptPath: "prompts/f-plt-16.md",
		Fixture:    "testdata/ai/f-plt-16/request-documents",
	}
}
