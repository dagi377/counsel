package readiness

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionReplyToClientMessage is the mobile-app action that runs F-RDY-07 on request.
const ActionReplyToClientMessage = "mobile-app > Reply to client text or portal message"

//go:embed prompts/f-rdy-07.md
var promptFRDY07 embed.FS

// ReplyToClientMessageDef is F-RDY-07: on request from the mobile app, the agent finds the
// client's text or portal message on the selected matter with search_documents, drafts a
// reply to that client with draft_email (never submitted for sending itself) and opens gate
// platform.portal_message with submit_for_approval. The reply is a draft only; it goes to the
// client after the same send gate as on the web, decided by a person other than the acting
// user.
func ReplyToClientMessageDef() feature.Def {
	return feature.Def{
		ID:      "F-RDY-07",
		Area:    "readiness",
		Name:    "Reply to client text or portal message",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionReplyToClientMessage,
		Actions: []string{ActionReplyToClientMessage},
		Tools:   []string{"search_documents", "draft_email", "report_plan", "report_step", "submit_for_approval"},
		// rule: ARC-I-035
		// docs.GatePortalMessage: submit_for_approval opens it on the email draft.
		GateKinds: []string{"platform.portal_message"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFRDY07,
		PromptPath: "prompts/f-rdy-07.md",
		Fixture:    "testdata/ai/f-rdy-07/reply-to-client-text-or-portal-message",
	}
}
