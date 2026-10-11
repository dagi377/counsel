package deadlines

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

//go:embed prompts/f-dl-06.md
var promptFDL06 embed.FS

// RuleWatchDef is F-DL-06: once a week the agent checks the rule packs in use against the
// rule sources it can read and drafts a new rule-pack version where a rule changed. The draft
// goes to cal.rule_pack, a tool-gap kind, through draft_document and submit_for_approval.
func RuleWatchDef() feature.Def {
	return feature.Def{
		ID:      "F-DL-06",
		Area:    "deadlines",
		Name:    "Rule watch",
		Source:  feature.SourceJob,
		Trigger: "schedule: weekly",
		Tools: []string{
			"get_matter", "read_document", "list_deadlines", "compute_deadline",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds:  []string{"cal.rule_pack"},
		Prompt:     promptFDL06,
		PromptPath: "prompts/f-dl-06.md",
		Fixture:    "testdata/ai/f-dl-06/rule-watch",
	}
}

// RuleWatchBinding binds F-DL-06 to its weekly schedule. A scheduled run has no event user,
// so it acts for the feature's designated lawyer in firm settings; with none named the run
// does not start and the readiness check reports it.
func RuleWatchBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(RuleWatchDef())
}
