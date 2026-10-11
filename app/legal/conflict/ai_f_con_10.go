package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// GateIDDestruction is the gate kind of spec gate G14: identity records past their retention
// period are destroyed only after people approve the batch.
const GateIDDestruction = "conflict.id_destruction"

//go:embed prompts/f-con-10.md
var promptFCON10 embed.FS

// ExpiryRetentionDef is F-CON-10: each week the agent finds identity records whose retention
// period has ended, drafts a destruction batch with draft_document and opens gate
// conflict.id_destruction (G14), a tool-gap kind, with submit_for_approval. People decide;
// nothing is destroyed by the run.
func ExpiryRetentionDef() feature.Def {
	return feature.Def{
		ID:      "F-CON-10",
		Area:    "conflicts",
		Name:    "Expiry and retention",
		Source:  feature.SourceJob,
		Trigger: "schedule: weekly",
		Tools: []string{
			"search_contacts", "search_matters", "read_document", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateIDDestruction},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFCON10,
		PromptPath: "prompts/f-con-10.md",
		Fixture:    "testdata/ai/f-con-10/expiry-and-retention",
	}
}

// ExpiryRetentionBinding binds F-CON-10 to its weekly schedule. A scheduled run has no event
// user, so it acts for the feature's designated lawyer from firm settings; with none named the
// run does not start and readiness flags it.
func ExpiryRetentionBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(ExpiryRetentionDef())
}
