package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureRetainerWatch is the spec id of "Retainer and budget watch".
const FeatureRetainerWatch = "F-MAT-08"

// Kernel events that start F-MAT-08: a matter's trust balance or its unbilled work in
// progress changed.
const (
	EventTrustBalanceChanged = "trust.balance_changed"
	EventWIPChanged          = "timekeeping.wip_changed"
)

// Matter financial summary actions that run F-MAT-08 on request.
const (
	ActionRequestTopUp      = "matter-financial-summary > Request retainer top-up"
	ActionFlagBudgetOverrun = "matter-financial-summary > Flag budget overrun"
)

//go:embed prompts/f-mat-08.md
var promptFMAT08 embed.FS

// RetainerWatchDef is F-MAT-08: when a matter's trust balance or work in progress changes,
// the agent checks the retainer and the budget and drafts a top-up request or a budget update
// email to the client. draft_email with submit opens platform.outgoing_email (G-OUT); nothing
// is sent until a person other than the preparer approves.
func RetainerWatchDef() feature.Def {
	return feature.Def{
		ID:      FeatureRetainerWatch,
		Area:    "matters",
		Name:    "Retainer and budget watch",
		Source:  feature.SourceJob,
		Trigger: "event: trust or WIP change",
		Actions: []string{ActionRequestTopUp, ActionFlagBudgetOverrun},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"draft_email", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{"platform.outgoing_email"}, // G-OUT, opened by draft_email with submit
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "contacts.view", "documents.edit", "documents.view", "matters.view"},
		Prompt:     promptFMAT08,
		PromptPath: "prompts/f-mat-08.md",
		Fixture:    "testdata/ai/f-mat-08/retainer-and-budget-watch",
	}
}

// RetainerWatchBinding binds F-MAT-08 to the trust and WIP change events. Event runs act for
// the event's user, so it needs no scheduled acting user.
func RetainerWatchBinding() (trigger.Binding, error) {
	return trigger.Bind(RetainerWatchDef(), "", EventTrustBalanceChanged, EventWIPChanged)
}
