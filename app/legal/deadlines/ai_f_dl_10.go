package deadlines

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-dl-10.md
var promptFDL10 embed.FS

// ProposeMeetingDef is F-DL-10: on the firm calendar the agent proposes a meeting time for the matter team that clears
// the matter's deadlines. It is read-only: no write tool, no gate. The answer goes back through the
// screen-action route only and is never stored (AI-R-005).
func ProposeMeetingDef() feature.Def {
	return feature.Def{
		ID:         "F-DL-10",
		Area:       "deadlines",
		Name:       "Propose meeting time",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: firm-calendar > Propose meeting time",
		Actions:    []string{"firm-calendar > Propose meeting time"},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), readTools...),
		Grant:      append([]string(nil), readGrant...),
		Prompt:     promptFDL10,
		PromptPath: "prompts/f-dl-10.md",
		Fixture:    "testdata/ai/f-dl-10/propose-meeting-time",
	}
}
