package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventReceptionistConversationStarted is the kernel event intake publishes when an inbound
// call or chat reaches the AI receptionist (a row in intake.receptionist_conversation). It
// starts F-INT-10.
const EventReceptionistConversationStarted = "receptionist_conversation.started"

// ActionAnswerAndCollect is the ai-receptionist action that runs F-INT-10 to answer the caller
// and collect the lead's details.
const ActionAnswerAndCollect = "ai-receptionist > Answer and collect"

// ActionOfferConsultSlot is the ai-receptionist action that runs F-INT-10 to offer the caller a
// consult slot.
const ActionOfferConsultSlot = "ai-receptionist > Offer consult slot"

//go:embed prompts/f-int-10.md
var promptFINT10 embed.FS

// AIReceptionistDef is F-INT-10: on an inbound call or chat, the agent answers, looks the
// caller up with search_contacts and search_matters, records the lead with its transcript
// through draft_document and opens gate intake.lead_decision (G1), a tool-gap kind, with
// submit_for_approval. A lawyer accepts or declines the lead; the agent never does.
func AIReceptionistDef() feature.Def {
	return feature.Def{
		ID:      "F-INT-10",
		Area:    "intake",
		Name:    "AI receptionist",
		Source:  feature.SourceJob,
		Trigger: "event: inbound call or chat",
		Actions: []string{ActionAnswerAndCollect, ActionOfferConsultSlot},
		Tools: []string{
			"search_contacts", "search_matters", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// intake.lead_decision is spec gate G1, the lawyer's accept-or-decline of a lead.
		GateKinds: []string{"intake.lead_decision"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.edit", "agent.ask"},
		Prompt:     promptFINT10,
		PromptPath: "prompts/f-int-10.md",
		Fixture:    "testdata/ai/f-int-10/ai-receptionist",
	}
}

// AIReceptionistBinding binds F-INT-10 to the inbound call or chat event. Event runs act for
// the event's user, so it needs no scheduled acting user.
func AIReceptionistBinding() (trigger.Binding, error) {
	return trigger.Bind(AIReceptionistDef(), "", EventReceptionistConversationStarted)
}
