package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-wrk-05.md
var promptFWRK05 embed.FS

// wrkdReadTools are the four read tools the read-only work features call, and wrkdReadGrant
// the capabilities their runs hold: each tool's capability and nothing more.
var (
	wrkdReadTools = []string{agent.ToolGetMatter, agent.ToolListWorkItems, agent.ToolListDocuments, agent.ToolReadDocument}
	wrkdReadGrant = []string{agent.CapMattersView, agent.CapWorkView, agent.CapDocumentsView}
)

// DailyDigestDef is F-WRK-05: each business morning the agent writes the work digest of the
// items the acting lawyer can see. It is read-only: no write tool, no gate, and the digest
// text goes back to the caller only (AI-R-005).
func DailyDigestDef() feature.Def {
	return feature.Def{
		ID:         "F-WRK-05",
		Area:       "work",
		Name:       "Daily work digest",
		Source:     feature.SourceJob,
		Trigger:    "schedule: weekdays 07:30",
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), wrkdReadTools...),
		Grant:      append([]string(nil), wrkdReadGrant...),
		Prompt:     promptFWRK05,
		PromptPath: "prompts/f-wrk-05.md",
		Fixture:    "testdata/ai/f-wrk-05/daily-work-digest",
	}
}

// DailyDigestBinding binds F-WRK-05 to its weekday schedule. A scheduled run has no event
// user, so it acts for the feature's designated lawyer from firm settings; with none named the
// run does not start and readiness flags it.
func DailyDigestBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(DailyDigestDef())
}
