package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// EventAutomationRunFailed is the kernel event work publishes when an automation run fails.
// It is one of the two events that start F-WRK-08.
const EventAutomationRunFailed = "work.automation_run_failed"

// EventAutomationRunSkipped is the kernel event work publishes when an automation run is
// skipped (its condition did not hold or a guard stopped it). It is the other event that
// starts F-WRK-08.
const EventAutomationRunSkipped = "work.automation_run_skipped"

//go:embed prompts/f-wrk-08.md
var promptFWRK08 embed.FS

// ExplainAutomationRunsDef is F-WRK-08: when an automation run fails or is skipped, the agent
// explains why on the run history, from the matter, its work items and documents it can read.
// It is read-only: no write tool, no gate, and the explanation goes back to the caller only
// (AI-R-005).
func ExplainAutomationRunsDef() feature.Def {
	return feature.Def{
		ID:         "F-WRK-08",
		Area:       "work",
		Name:       "Explain automation runs",
		Source:     feature.SourceJob,
		Trigger:    "event: automation run failed or skipped",
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), wrkdReadTools...),
		Grant:      append([]string(nil), wrkdReadGrant...),
		Prompt:     promptFWRK08,
		PromptPath: "prompts/f-wrk-08.md",
		Fixture:    "testdata/ai/f-wrk-08/explain-automation-runs",
	}
}

// ExplainAutomationRunsBinding binds F-WRK-08 to the failed and skipped automation run events.
// Event runs act for the event's user, so it needs no scheduled acting user.
func ExplainAutomationRunsBinding() (trigger.Binding, error) {
	return trigger.Bind(ExplainAutomationRunsDef(), "", EventAutomationRunFailed, EventAutomationRunSkipped)
}
