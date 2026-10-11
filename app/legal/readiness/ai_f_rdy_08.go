package readiness

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionDraftReply is the mail add-in action that runs F-RDY-08 on request.
const ActionDraftReply = "mail-add-ins > Draft reply"

//go:embed prompts/f-rdy-08.md
var promptFRDY08 embed.FS

// DraftReplyDef is F-RDY-08: from the mail add-in, the agent finds the selected email, filed
// on its matter, with search_documents and drafts the reply with draft_email, submitted to
// gate platform.outgoing_email: the documents release gate every outgoing email passes on the
// web. A person other than the acting user approves it before anything is sent.
func DraftReplyDef() feature.Def {
	return feature.Def{
		ID:      "F-RDY-08",
		Area:    "readiness",
		Name:    "Draft reply",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionDraftReply,
		Actions: []string{ActionDraftReply},
		Tools:   []string{"search_documents", "draft_email", "report_plan", "report_step", "submit_for_approval"},
		// rule: ARC-I-035
		// agent.GateOutgoingEmail: draft_email with submit true opens it on the email draft.
		GateKinds: []string{"platform.outgoing_email"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFRDY08,
		PromptPath: "prompts/f-rdy-08.md",
		Fixture:    "testdata/ai/f-rdy-08/draft-reply",
	}
}
