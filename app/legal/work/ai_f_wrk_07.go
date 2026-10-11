package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// ActionSuggestRules is the work-automations screen action that runs F-WRK-07 on request.
const ActionSuggestRules = "work-automations > Suggest rules"

// GateAutomation is the gate kind of spec gate G-AR: a person with work.workflows enables an
// automation rule.
const GateAutomation = "work.automation"

//go:embed prompts/f-wrk-07.md
var promptFWRK07 embed.FS

// RuleSuggesterDef is F-WRK-07: once a week the agent reads the matter's work items and
// documents, proposes each automation rule it finds worth adding as a task with create_task
// and opens gate work.automation (G-AR) on it with submit_for_approval. A rule is enabled
// only once a person with work.workflows approves; the agent never enables one.
func RuleSuggesterDef() feature.Def {
	return feature.Def{
		ID:      "F-WRK-07",
		Area:    "work",
		Name:    "Rule suggester",
		Source:  feature.SourceJob,
		Trigger: "schedule: weekly",
		Actions: []string{ActionSuggestRules},
		Tools: []string{
			"get_matter", "list_work_items", "list_documents", "read_document",
			"create_task", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateAutomation},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "work.view", "documents.view", "work.edit", "agent.ask"},
		Prompt:     promptFWRK07,
		PromptPath: "prompts/f-wrk-07.md",
		Fixture:    "testdata/ai/f-wrk-07/rule-suggester",
	}
}

// RuleSuggesterBinding binds F-WRK-07 to its weekly run, which acts for the feature's
// designated lawyer in firm settings; with none set the run does not start (JOB-ACTOR).
func RuleSuggesterBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(RuleSuggesterDef())
}
