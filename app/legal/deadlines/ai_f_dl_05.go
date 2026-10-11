package deadlines

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-dl-05.md
var promptFDL05 embed.FS

// readTools are the four read tools the read-only deadline features call, and readGrant the
// capabilities their runs hold: each tool's a2 capability and nothing more.
var (
	readTools = []string{agent.ToolGetMatter, agent.ToolReadDocument, agent.ToolListDeadlines, "compute_deadline"}
	readGrant = []string{agent.CapMattersView, agent.CapDocumentsView, agent.CapDeadlinesView}
)

// DigestDef is F-DL-05: each business morning the agent writes the deadline digest, and on
// Mondays the weekly report, from the stored deadlines it can read. It is read-only: no write
// tool, no gate, and the digest text goes back to the caller only (AI-R-005).
func DigestDef() feature.Def {
	return feature.Def{
		ID:         "F-DL-05",
		Area:       "deadlines",
		Name:       "Daily digest and weekly report",
		Source:     feature.SourceJob,
		Trigger:    "schedule: weekdays 07:30, Mondays 07:00",
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), readTools...),
		Grant:      append([]string(nil), readGrant...),
		Prompt:     promptFDL05,
		PromptPath: "prompts/f-dl-05.md",
		Fixture:    "testdata/ai/f-dl-05/daily-digest-and-weekly-report",
	}
}

// DigestBinding binds F-DL-05 to its schedule: every business day, which covers the Monday
// report. A scheduled run has no event user, so it acts for the feature's designated lawyer
// from firm settings; with none named the run does not start and readiness flags it.
func DigestBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(DigestDef())
}
