package access

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// FeatureSuggestTeam is the spec id of "Suggest team".
const FeatureSuggestTeam = "F-ACC-10"

// GateMatterVisibility is the gate that changes who may see a matter. No module registers it
// yet; F-ACC-10 opens it as a tool-gap kind.
const GateMatterVisibility = "core.matter_visibility"

// suggestTeamTrigger is how features.json words F-ACC-10's start: a screen action only.
const suggestTeamTrigger = "on request: matter-access > Suggest team"

//go:embed prompts/f-acc-10.md
var promptFACC10 embed.FS

// SuggestTeamDef is F-ACC-10: on the matter-access screen the agent proposes the matter's
// team from the practice area and the lawyers' workload, drafts it with draft_document and
// submits the draft for gate core.matter_visibility with submit_for_approval. The gate's
// preparer is the acting user; a person other than the preparer decides it and the agent
// never does.
func SuggestTeamDef() feature.Def {
	return feature.Def{
		ID:      FeatureSuggestTeam,
		Area:    "access",
		Name:    "Suggest team",
		Source:  feature.SourceScreenAction,
		Trigger: suggestTeamTrigger,
		// The screen action is the trigger itself: "matter-access > Suggest team".
		Actions: []string{strings.TrimPrefix(suggestTeamTrigger, "on request: ")},
		Tools:   []string{"search_matters", "draft_document", "report_plan", "report_step", "submit_for_approval"},
		// rule: ARC-I-035
		// core.matter_visibility has no dedicated write tool: the run drafts the team and submits it.
		GateKinds: []string{GateMatterVisibility},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "documents.edit", "matters.view"},
		Prompt:     promptFACC10,
		PromptPath: "prompts/f-acc-10.md",
		Fixture:    "testdata/ai/f-acc-10/suggest-team",
	}
}
