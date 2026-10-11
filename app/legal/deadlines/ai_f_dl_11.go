package deadlines

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-dl-11.md
var promptFDL11 embed.FS

// FindTeamTimeDef is F-DL-11: on the firm calendar the agent finds a time for the matter team, skipping court
// holidays and the days the matter's deadlines take. It is read-only: no write tool, no gate. The answer goes back through the
// screen-action route only and is never stored (AI-R-005).
func FindTeamTimeDef() feature.Def {
	return feature.Def{
		ID:         "F-DL-11",
		Area:       "deadlines",
		Name:       "Find a time for the matter team",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: firm-calendar > Find a time for the matter team",
		Actions:    []string{"firm-calendar > Find a time for the matter team"},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), readTools...),
		Grant:      append([]string(nil), readGrant...),
		Prompt:     promptFDL11,
		PromptPath: "prompts/f-dl-11.md",
		Fixture:    "testdata/ai/f-dl-11/find-a-time-for-the-matter-team",
	}
}
