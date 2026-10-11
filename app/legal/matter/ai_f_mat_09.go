package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-mat-09.md
var promptFMAT09 embed.FS

// FeatureConfigHygiene is the spec id of "Config hygiene".
const FeatureConfigHygiene = "F-MAT-09"

// GatePracticeConfig is G-CF: a proposed change to a practice area's configuration (stages,
// matter fields, templates, starter set). It is a tool-gap kind: the run drafts the proposal
// with draft_document and opens the gate with submit_for_approval.
const GatePracticeConfig = "matter.practice_config"

// ActionSuggestStarterSet is the practice-area-settings screen action that runs F-MAT-09 on
// request.
const ActionSuggestStarterSet = "practice-area-settings > Suggest starter set"

// SubjectAgentDraft is the submit_for_approval subject type of a draft a matter feature run
// made with draft_document.
const SubjectAgentDraft = "draft"

// ConfigHygieneDef is F-MAT-09: once a month, or on request from practice-area settings, the
// agent reviews how a practice area's configuration is used on its matters and drafts config
// proposals for G-CF. It never changes the configuration itself.
func ConfigHygieneDef() feature.Def {
	return feature.Def{
		ID:      FeatureConfigHygiene,
		Area:    "matters",
		Name:    "Config hygiene",
		Source:  feature.SourceJob,
		Trigger: "schedule: monthly",
		Actions: []string{ActionSuggestStarterSet},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds:  []string{GatePracticeConfig},
		Grant:      []string{"agent.ask", "contacts.view", "documents.edit", "documents.view", "matters.view"},
		Prompt:     promptFMAT09,
		PromptPath: "prompts/f-mat-09.md",
		Fixture:    "testdata/ai/f-mat-09/config-hygiene",
	}
}

// ConfigHygieneBinding binds F-MAT-09 to its monthly schedule. A scheduled run has no event
// user, so it acts for the feature's designated lawyer in firm settings; with none named the
// run does not start and the readiness check reports it. The screen action runs for the user
// who asked.
func ConfigHygieneBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(ConfigHygieneDef())
}
