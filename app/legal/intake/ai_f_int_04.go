package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// Kernel events the lead module publishes when a lawyer decides a lead. Either starts F-INT-04.
const (
	// EventLeadDeclined: a lawyer chose to decline the lead.
	EventLeadDeclined = "lead.declined"
	// EventLeadReferred: a lawyer chose to refer the lead elsewhere.
	EventLeadReferred = "lead.referred"
)

//go:embed prompts/f-int-04.md
var promptFINT04 embed.FS

// DeclineReferLetterDef is F-INT-04: when a lawyer declines or refers a lead, the agent drafts
// the letter with draft_document and opens gate intake.lead_decision (G1), a tool-gap kind,
// with submit_for_approval. Nothing is sent until a lawyer approves.
func DeclineReferLetterDef() feature.Def {
	return feature.Def{
		ID:      "F-INT-04",
		Area:    "intake",
		Name:    "Decline/refer letter drafter",
		Source:  feature.SourceJob,
		Trigger: "event: lawyer chooses decline or refer",
		Tools: []string{
			"search_contacts", "search_matters", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateLeadDecision},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.edit", "agent.ask"},
		Prompt:     promptFINT04,
		PromptPath: "prompts/f-int-04.md",
		Fixture:    "testdata/ai/f-int-04/decline-refer-letter-drafter",
	}
}

// DeclineReferLetterBinding binds F-INT-04 to the decline and refer events. Event runs act for
// the lawyer who decided, so it needs no scheduled acting user.
func DeclineReferLetterBinding() (trigger.Binding, error) {
	return trigger.Bind(DeclineReferLetterDef(), "", EventLeadDeclined, EventLeadReferred)
}
