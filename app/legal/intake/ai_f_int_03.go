package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventLeadAwaitingInfo is the event intake publishes when a lead is set to wait for
// information from the prospective client. It starts F-INT-03.
const EventLeadAwaitingInfo = "intake.lead_awaiting_info"

// ActionRequestMissingInfo is the lead-detail screen action that runs F-INT-03 on request.
const ActionRequestMissingInfo = "lead-detail > Request missing info"

//go:embed prompts/f-int-03.md
var promptFINT03 embed.FS

// MissingInfoChaserDef is F-INT-03: for a lead waiting on information, the agent works out
// which lead fields are still missing, drafts the updated fields and the request for the rest
// with draft_document, and opens gate platform.skill_autonomy, a tool-gap kind, with
// submit_for_approval. A person approves; the run changes no lead and sends nothing.
func MissingInfoChaserDef() feature.Def {
	return feature.Def{
		ID:      "F-INT-03",
		Area:    "intake",
		Name:    "Missing-info chaser",
		Source:  feature.SourceJob,
		Trigger: "event: lead awaiting info; schedule",
		Actions: []string{ActionRequestMissingInfo},
		Tools: []string{
			"search_contacts", "search_matters", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{"platform.skill_autonomy"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.edit", "agent.ask"},
		Prompt:     promptFINT03,
		PromptPath: "prompts/f-int-03.md",
		Fixture:    "testdata/ai/f-int-03/missing-info-chaser",
	}
}

// MissingInfoChaserBinding binds F-INT-03 to a lead set to await information and to its daily
// schedule. A scheduled run has no event user, so it acts for the feature's designated lawyer,
// read from firm settings at each tick; event runs act for the event's user. The on-request
// start is the Def's screen action, served by trigger.Actions.
func MissingInfoChaserBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(MissingInfoChaserDef(), EventLeadAwaitingInfo)
}
