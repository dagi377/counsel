package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventClearanceSubjectToConsent is the kernel event the clearance module publishes when a
// conflict check is cleared subject to consent. It starts F-CON-04.
const EventClearanceSubjectToConsent = "conflict.clearance_subject_to_consent"

// ActionDraftConsentLetter is the consents-waivers screen action that runs F-CON-04 on request.
const ActionDraftConsentLetter = "consents-waivers > Draft consent letter"

// GateConsentText is the gate kind of spec gate G2a: a lawyer approves the consent letter text
// before it is sent.
const GateConsentText = "conflict.consent_text"

//go:embed prompts/f-con-04.md
var promptFCON04 embed.FS

// ConsentDraftingDef is F-CON-04: when a check is cleared subject to consent, the agent drafts
// the consent letters with draft_document and opens gate conflict.consent_text (G2a), a
// tool-gap kind, with submit_for_approval. Nothing is sent until a lawyer approves.
func ConsentDraftingDef() feature.Def {
	return feature.Def{
		ID:      "F-CON-04",
		Area:    "conflicts",
		Name:    "Consent drafting and chasing",
		Source:  feature.SourceJob,
		Trigger: "event: clearance outcome 'subject to consent'",
		Actions: []string{ActionDraftConsentLetter},
		Tools: []string{
			"search_contacts", "search_matters", "read_document", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateConsentText},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFCON04,
		PromptPath: "prompts/f-con-04.md",
		Fixture:    "testdata/ai/f-con-04/consent-drafting-and-chasing",
	}
}

// ConsentDraftingBinding binds F-CON-04 to the subject-to-consent clearance event. Event runs
// act for the event's user, so it needs no scheduled acting user.
func ConsentDraftingBinding() (trigger.Binding, error) {
	return trigger.Bind(ConsentDraftingDef(), "", EventClearanceSubjectToConsent)
}
