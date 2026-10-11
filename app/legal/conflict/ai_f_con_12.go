package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionProposeDuplicates is the party-records-hygiene screen action that runs F-CON-12 on
// request.
const ActionProposeDuplicates = "party-records-hygiene > Propose duplicates and relationships"

//go:embed prompts/f-con-12.md
var promptFCON12 embed.FS

// DuplicateFinderDef is F-CON-12: each night the agent ranks party records that look like
// duplicates or related entities and suggests them on the party-records-hygiene screen, where
// a lawyer approves any merge. It is read-only: no write tool, no gate, and the suggestions go
// back to the caller only (AI-R-005).
func DuplicateFinderDef() feature.Def {
	return feature.Def{
		ID:         "F-CON-12",
		Area:       "conflicts",
		Name:       "Duplicate and relationship finder",
		Source:     feature.SourceJob,
		Trigger:    "schedule: nightly",
		Actions:    []string{ActionProposeDuplicates},
		Kind:       ai.KindEmbedding,
		Tools:      append([]string(nil), readOnlyTools...),
		Grant:      append([]string(nil), readOnlyGrant...),
		Prompt:     promptFCON12,
		PromptPath: "prompts/f-con-12.md",
		Fixture:    "testdata/ai/f-con-12/duplicate-and-relationship-finder",
	}
}

// DuplicateFinderBinding binds F-CON-12 to its nightly schedule (a daily business-day
// cadence). A scheduled run has no event user, so boot names the user it acts for; empty is
// trigger.ErrBadBinding.
func DuplicateFinderBinding(actingFor string) (trigger.Binding, error) {
	return trigger.Bind(DuplicateFinderDef(), actingFor)
}
