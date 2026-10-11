package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeaturePartyExtraction is the spec id of "Party extraction".
const FeaturePartyExtraction = "F-CON-01"

// Kernel event types that start F-CON-01 (spec/eng/a2-interfaces.json events). An intake is
// an intake-module lead, so "intake submitted" is lead.created; "document added to a matter"
// is the docs module's document.uploaded.
const (
	EventIntakeSubmitted = "lead.created"
	EventDocumentAdded   = "document.uploaded"
)

//go:embed prompts/f-con-01.md
var promptFCON01 embed.FS

// PartyExtractionDef is F-CON-01: the agent reads the intake or the added document and
// proposes the CheckParty list for a new conflict check. The check run waits on G2
// (conflict.clearance), prepared by the acting user; a lawyer other than them clears it.
func PartyExtractionDef() feature.Def {
	return feature.Def{
		ID:      FeaturePartyExtraction,
		Area:    "conflicts",
		Name:    "Party extraction",
		Source:  feature.SourceJob,
		Trigger: "event: intake submitted or document added to a matter",
		Actions: []string{"conflict-check-new > Suggest parties"},
		Kind:    ai.KindExtraction,
		Tools: []string{
			"search_contacts", "search_matters", "read_document", "propose_conflict_parties",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds:  []string{GateClearance},
		Grant:      []string{"agent.ask", "conflicts.run", "contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFCON01,
		PromptPath: "prompts/f-con-01.md",
		Fixture:    "testdata/ai/f-con-01/party-extraction",
	}
}

// PartyExtractionBinding binds F-CON-01 to an intake submitted or a document added to a
// matter. Event runs act for the event's user. The on-request start is the Def's screen
// action, "conflict-check-new > Suggest parties", served by trigger.Actions.
func PartyExtractionBinding() (trigger.Binding, error) {
	return trigger.Bind(PartyExtractionDef(), "", EventIntakeSubmitted, EventDocumentAdded)
}
