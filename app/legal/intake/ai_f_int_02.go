package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// EventEmailReceived is the event a connected mailbox emits when an email arrives, with the
// message's ids and the mailbox owner as user_id (the event F-DOC-08 also starts on). F-INT-02
// starts on it; the run drafts nothing for mail not sent to the intake address.
const EventEmailReceived = "email.received"

// ActionExtractLead is the phone-and-email-capture screen action that runs F-INT-02 on
// request.
const ActionExtractLead = "phone-and-email-capture > Extract lead"

//go:embed prompts/f-int-02.md
var promptFINT02 embed.FS

// MailboxWatcherDef is F-INT-02: when mail reaches the intake address, the agent extracts a
// draft lead with draft_document and opens gate intake.lead_decision, a tool-gap kind, with
// submit_for_approval. Staff review the draft; the run creates no lead itself.
func MailboxWatcherDef() feature.Def {
	return feature.Def{
		ID:      "F-INT-02",
		Area:    "intake",
		Name:    "Intake mailbox watcher",
		Source:  feature.SourceJob,
		Trigger: "event: mail.received on the intake address",
		Actions: []string{ActionExtractLead},
		Kind:    ai.KindExtraction,
		Tools: []string{
			"search_contacts", "search_matters", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{"intake.lead_decision"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.edit", "agent.ask"},
		Prompt:     promptFINT02,
		PromptPath: "prompts/f-int-02.md",
		Fixture:    "testdata/ai/f-int-02/intake-mailbox-watcher",
	}
}

// MailboxWatcherBinding binds F-INT-02 to arriving mail. Event runs act for the event's user,
// so it needs no scheduled acting user. The on-request start is the Def's screen action,
// served by trigger.Actions.
func MailboxWatcherBinding() (trigger.Binding, error) {
	return trigger.Bind(MailboxWatcherDef(), "", EventEmailReceived)
}
