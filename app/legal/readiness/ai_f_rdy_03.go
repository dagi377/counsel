package readiness

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/deadline"
)

//go:embed prompts/f-rdy-03.md
var promptFRDY03 embed.FS

// RulePackImpactDef is F-RDY-03: when a new rule pack version is published, the agent finds
// the deadlines it changes from the pack notice and the matter's records with
// search_documents, and proposes each recomputed date on the stored deadline with
// propose_deadline, which opens gate cal.change. The stored date stays until a person other
// than the acting user approves; every proposed date is one a search returned.
func RulePackImpactDef() feature.Def {
	return feature.Def{
		ID:      "F-RDY-03",
		Area:    "readiness",
		Name:    "Rule pack impact",
		Source:  feature.SourceJob,
		Trigger: "event",
		Tools: []string{
			agent.ToolSearchDocuments, agent.ToolProposeDeadline,
			agent.ToolReportPlan, agent.ToolReportStep, agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{agent.GateCalChange},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapDocumentsView, "deadlines.edit", agent.CapAsk},
		Prompt:     promptFRDY03,
		PromptPath: "prompts/f-rdy-03.md",
		Fixture:    "testdata/ai/f-rdy-03/rule-pack-impact",
	}
}

// RulePackImpactBinding binds F-RDY-03 to the kernel's rule pack published event. The run
// acts for the event's user, so it has no schedule.
func RulePackImpactBinding() (trigger.Binding, error) {
	return trigger.Bind(RulePackImpactDef(), "", deadline.EventPackPublished)
}
