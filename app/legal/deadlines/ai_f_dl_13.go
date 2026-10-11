package deadlines

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-dl-13.md
var promptFDL13 embed.FS

// DraftAgendaDef is F-DL-13: on an event's detail page the agent drafts the meeting agenda from the matter brief
// and its open deadlines. The draft is an answer on screen only; nothing is saved. It is read-only: no write tool, no gate. The answer goes back through the
// screen-action route only and is never stored (AI-R-005).
func DraftAgendaDef() feature.Def {
	return feature.Def{
		ID:         "F-DL-13",
		Area:       "deadlines",
		Name:       "Draft agenda",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: event-detail > Draft agenda",
		Actions:    []string{"event-detail > Draft agenda"},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), readTools...),
		Grant:      append([]string(nil), readGrant...),
		Prompt:     promptFDL13,
		PromptPath: "prompts/f-dl-13.md",
		Fixture:    "testdata/ai/f-dl-13/draft-agenda",
	}
}
