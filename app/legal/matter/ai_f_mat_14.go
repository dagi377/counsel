package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-mat-14.md
var promptFMAT14 embed.FS

// FeatureTemplateFit is the spec id of "Template fit".
const FeatureTemplateFit = "F-MAT-14"

// GateApplyTemplate is G-AT: a matter template is applied to a matter. It is a tool-gap
// kind: the run drafts the suggestion with draft_document and opens the gate with
// submit_for_approval.
const GateApplyTemplate = "matter.apply_template"

// ActionSuggestTemplate is the matter-overview screen action that runs F-MAT-14 on request.
const ActionSuggestTemplate = "matter-overview > Suggest a template"

// EventMatterOpened is the kernel event of a newly opened matter (spec/eng/a2-interfaces.json).
// The run reads the matter and suggests nothing when it already has a template.
const EventMatterOpened = "matter.opened"

// templateFitSchedule is how the binding reads the spec's "weekly sweep": trigger.ParseCadence
// finds a schedule only where the trigger says "schedule" (docs/QUESTIONS.md Q-FEAT-MAT-12).
const templateFitSchedule = "schedule: weekly sweep"

// TemplateFitDef is F-MAT-14: when a matter opens without a template, in a weekly sweep of
// such matters, or on request from the matter overview, the agent suggests the template that
// fits the matter for G-AT. It never applies a template itself.
func TemplateFitDef() feature.Def {
	return feature.Def{
		ID:      FeatureTemplateFit,
		Area:    "matters",
		Name:    "Template fit",
		Source:  feature.SourceJob,
		Trigger: "event: matter opened without a template; weekly sweep",
		Actions: []string{ActionSuggestTemplate},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds:  []string{GateApplyTemplate},
		Grant:      []string{"agent.ask", "contacts.view", "documents.edit", "documents.view", "matters.view"},
		Prompt:     promptFMAT14,
		PromptPath: "prompts/f-mat-14.md",
		Fixture:    "testdata/ai/f-mat-14/template-fit",
	}
}

// TemplateFitBinding binds F-MAT-14 to matter.opened and to its weekly sweep. An event run
// acts for the event's user; the sweep has no event user, so it acts for the feature's
// designated lawyer in firm settings, and with none named it does not start and the
// readiness check reports it.
func TemplateFitBinding() (trigger.Binding, error) {
	d := TemplateFitDef()
	d.Trigger = templateFitSchedule
	return trigger.BindDesignated(d, EventMatterOpened)
}
