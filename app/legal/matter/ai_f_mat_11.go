package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-mat-11.md
var promptFMAT11 embed.FS

// FeatureAccessHygiene is the spec id of "Access hygiene".
const FeatureAccessHygiene = "F-MAT-11"

// The screen actions that run F-MAT-11 on request.
const (
	ActionFlagStuckMatter      = "matter-stage-board > Flag stuck matter"
	ActionFlagOversharing      = "matter-access > Flag oversharing"
	ActionFlagOverSharedMatter = "matter-access-report > Flag over-shared matters"
)

// AccessHygieneDef is F-MAT-11: once a week, or on request from the stage board, a matter's
// access screen or the access report, the agent flags matters shared wider than their
// practice area's default, people with access who never touched the file, and matters stuck
// in a stage. It is read-only: the flags go back as a cited answer and change no access.
func AccessHygieneDef() feature.Def {
	return feature.Def{
		ID:      FeatureAccessHygiene,
		Area:    "matters",
		Name:    "Access hygiene",
		Source:  feature.SourceJob,
		Trigger: "schedule: weekly",
		Actions: []string{ActionFlagStuckMatter, ActionFlagOversharing, ActionFlagOverSharedMatter},
		Kind:    ai.KindChat,
		Tools:   []string{"get_matter", "search_documents", "read_document", "search_contacts"},
		// rule: AI-R-003
		// A read-only feature: no write tool, no gate kind, and the grant of its reads only.
		Grant:      []string{"contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFMAT11,
		PromptPath: "prompts/f-mat-11.md",
		Fixture:    "testdata/ai/f-mat-11/access-hygiene",
	}
}

// AccessHygieneBinding binds F-MAT-11 to its weekly schedule. A scheduled run has no event
// user, so it acts for the feature's designated lawyer in firm settings; with none named the
// run does not start and the readiness check reports it. The screen actions run for the user
// who asked.
func AccessHygieneBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(AccessHygieneDef())
}
