package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// readTools are the two read tools the read-only intake features call, and readGrant the
// capabilities their runs hold: each tool's a2 capability and nothing more.
var (
	readTools = []string{"search_contacts", "search_matters"}
	readGrant = []string{"contacts.view", "matters.view"}
)

//go:embed prompts/f-int-07.md
var promptFINT07 embed.FS

// PipelineFollowUpsDef is F-INT-07: each hour the agent reads the open leads and matters it
// can see and writes the pipeline follow-ups digest: the nudges due and the leads stuck in a
// stage. It is read-only: no write tool, no gate, and the digest goes back to the caller only
// (AI-R-005).
func PipelineFollowUpsDef() feature.Def {
	return feature.Def{
		ID:         "F-INT-07",
		Area:       "intake",
		Name:       "Pipeline follow-ups",
		Source:     feature.SourceJob,
		Trigger:    "schedule: hourly",
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), readTools...),
		Grant:      append([]string(nil), readGrant...),
		Prompt:     promptFINT07,
		PromptPath: "prompts/f-int-07.md",
		Fixture:    "testdata/ai/f-int-07/pipeline-follow-ups",
	}
}

// PipelineFollowUpsBinding binds F-INT-07 to its hourly schedule (business days). A scheduled
// run acts for the feature's designated lawyer in firm settings; with none named it does not
// start (JOB-ACTOR).
func PipelineFollowUpsBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(PipelineFollowUpsDef())
}
