package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// Kernel event types that start F-CON-06. The trust module publishes them.
const (
	EventTrustRequestDrafted = "trust.request_drafted"
	EventTrustFundsExpected  = "trust.funds_expected"
)

// GateClientID is the gate kind of spec gate G4: a person verifies the client's identity
// before funds move.
const GateClientID = "conflict.client_id"

//go:embed prompts/f-con-06.md
var promptFCON06 embed.FS

// IDObligationWatcherDef is F-CON-06: when a trust request is drafted or funds are expected,
// the agent checks the client's identification and verification record and, where it is
// missing or stale, drafts a verification task with draft_document and opens gate
// conflict.client_id (G4), a tool-gap kind, with submit_for_approval.
func IDObligationWatcherDef() feature.Def {
	return feature.Def{
		ID:      "F-CON-06",
		Area:    "conflicts",
		Name:    "ID obligation watcher",
		Source:  feature.SourceJob,
		Trigger: "event: trust request drafted or funds expected",
		Tools: []string{
			"search_contacts", "search_matters", "read_document", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateClientID},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFCON06,
		PromptPath: "prompts/f-con-06.md",
		Fixture:    "testdata/ai/f-con-06/id-obligation-watcher",
	}
}

// IDObligationWatcherBinding binds F-CON-06 to the trust events that start it. Event runs act
// for the event's user, so it needs no scheduled acting user.
func IDObligationWatcherBinding() (trigger.Binding, error) {
	return trigger.Bind(IDObligationWatcherDef(), "", EventTrustRequestDrafted, EventTrustFundsExpected)
}
