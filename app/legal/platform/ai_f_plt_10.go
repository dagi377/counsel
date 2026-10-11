package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventTextReceived is published when a client's text message arrives on a matter's text
// thread; it starts F-PLT-10 for the matter's responsible lawyer.
const EventTextReceived = "text.received"

// ActionDraftSubstantiveReply is the text-messaging action that runs F-PLT-10 on request.
const ActionDraftSubstantiveReply = "text-messaging > Draft substantive reply"

//go:embed prompts/f-plt-10.md
var promptFPLT10 embed.FS

// TextTriageDef is F-PLT-10: when a client's text arrives, or a lawyer asks on the
// text-messaging screen, the agent reads the matter and drafts a substantive reply with
// draft_email on that matter, submitted to gate platform.outgoing_email (G-OUT). Nothing is
// sent until a person other than the acting user approves it.
func TextTriageDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-10",
		Area:    "platform",
		Name:    "Text triage",
		Source:  feature.SourceJob,
		Trigger: "event (inbound text)",
		Actions: []string{ActionDraftSubstantiveReply},
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolDraftEmail,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{agent.GateOutgoingEmail},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView, "documents.edit", agent.CapAsk},
		Prompt:     promptFPLT10,
		PromptPath: "prompts/f-plt-10.md",
		Fixture:    "testdata/ai/f-plt-10/text-triage",
	}
}

// TextTriageBinding binds F-PLT-10 to the inbound text event. Event runs act for the event's
// user, so it needs no scheduled acting user.
func TextTriageBinding() (trigger.Binding, error) {
	return trigger.Bind(TextTriageDef(), "", EventTextReceived)
}
