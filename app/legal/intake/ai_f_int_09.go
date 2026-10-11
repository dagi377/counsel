package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionWeeklyIntakeDigest is the intake-dashboard screen action that runs F-INT-09 on
// request.
const ActionWeeklyIntakeDigest = "intake-dashboard > Weekly intake digest"

//go:embed prompts/f-int-09.md
var promptFINT09 embed.FS

// WeeklyIntakeDigestDef is F-INT-09: each Monday morning, or on request from the intake
// dashboard, the agent writes the internal weekly intake digest from the leads and matters it
// can see. It is read-only: no write tool, no gate, and the digest goes back to the caller
// only (AI-R-005).
func WeeklyIntakeDigestDef() feature.Def {
	return feature.Def{
		ID:         "F-INT-09",
		Area:       "intake",
		Name:       "Weekly intake digest",
		Source:     feature.SourceJob,
		Trigger:    "schedule: Monday 08:00 America/Toronto",
		Actions:    []string{ActionWeeklyIntakeDigest},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), readTools...),
		Grant:      append([]string(nil), readGrant...),
		Prompt:     promptFINT09,
		PromptPath: "prompts/f-int-09.md",
		Fixture:    "testdata/ai/f-int-09/weekly-intake-digest",
	}
}

// WeeklyIntakeDigestBinding binds F-INT-09 to its weekly schedule (the first business day of
// the week). A scheduled run acts for the feature's designated lawyer in firm settings; with
// none named it does not start (JOB-ACTOR). The on-request start is the Def's screen action,
// served by trigger.Actions.
func WeeklyIntakeDigestBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(WeeklyIntakeDigestDef())
}
