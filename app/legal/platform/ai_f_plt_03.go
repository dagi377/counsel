package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-plt-03.md
var promptFPLT03 embed.FS

// FeatureReplyDrafter is the spec id of "Reply drafter".
const FeatureReplyDrafter = "F-PLT-03"

// EventEmailNeedsReply is the event the comms module emits when an inbound email filed to a
// matter is marked as needing a response, with the email's matter and the mailbox owner as
// user_id. It starts F-PLT-03.
const EventEmailNeedsReply = "email.needs_reply"

// ActionDraftReply is the comms-log action that runs F-PLT-03 on request.
const ActionDraftReply = "comms-log > Draft reply"

// ReplyDrafterDef is F-PLT-03: when an inbound email needs a response, the agent reads the
// matter and its documents and drafts the reply with draft_email, submitted, which opens
// platform.outgoing_email (G-OUT) with the acting user as preparer. Nothing is sent until a
// person other than the preparer approves.
func ReplyDrafterDef() feature.Def {
	return feature.Def{
		ID:      FeatureReplyDrafter,
		Area:    "platform",
		Name:    "Reply drafter",
		Source:  feature.SourceJob,
		Trigger: "event: inbound email needing a response",
		Actions: []string{ActionDraftReply},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document",
			"draft_email", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// G-OUT is opened only by draft_email with submit, through the run's scoped opener.
		GateKinds: []string{agent.GateOutgoingEmail},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "documents.edit", "documents.view", "matters.view"},
		Prompt:     promptFPLT03,
		PromptPath: "prompts/f-plt-03.md",
		Fixture:    "testdata/ai/f-plt-03/reply-drafter",
	}
}

// ReplyDrafterBinding binds F-PLT-03 to an inbound email needing a response. Event runs act
// for the event's user (the mailbox owner), so it needs no scheduled acting user.
func ReplyDrafterBinding() (trigger.Binding, error) {
	return trigger.Bind(ReplyDrafterDef(), "", EventEmailNeedsReply)
}
