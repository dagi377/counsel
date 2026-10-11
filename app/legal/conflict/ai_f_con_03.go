package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventHitsDisposed is published when every hit of a conflict run has a disposition. It
// starts F-CON-03.
const EventHitsDisposed = "conflict.hits_disposed"

//go:embed prompts/f-con-03.md
var promptFCON03 embed.FS

// ClearanceMemoDef is F-CON-03: once every hit of a check is disposed, the agent confirms the
// check's parties and drafts the clearance memo a lawyer reads when deciding gate G2
// (conflict.clearance). The agent never decides it.
func ClearanceMemoDef() feature.Def {
	return feature.Def{
		ID:      "F-CON-03",
		Area:    "conflicts",
		Name:    "Clearance memo",
		Source:  feature.SourceJob,
		Trigger: "event: all hits disposed",
		Actions: []string{"conflict-clearance > Draft memo"},
		Tools: []string{
			"search_contacts", "search_matters", "read_document", "propose_conflict_parties",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds:  []string{GateClearance},
		Grant:      []string{"agent.ask", "conflicts.run", "contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFCON03,
		PromptPath: "prompts/f-con-03.md",
		Fixture:    "testdata/ai/f-con-03/clearance-memo",
	}
}

// ClearanceMemoBinding binds F-CON-03 to the hits-disposed event. Event runs act for the
// event's user, so it needs no scheduled acting user.
func ClearanceMemoBinding() (trigger.Binding, error) {
	return trigger.Bind(ClearanceMemoDef(), "", EventHitsDisposed)
}
