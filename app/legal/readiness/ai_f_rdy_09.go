package readiness

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionAsk is the research action that runs F-RDY-09 on request.
const ActionAsk = "research > Ask"

//go:embed prompts/f-rdy-09.md
var promptFRDY09 embed.FS

// AskDef is F-RDY-09: on the research screen the agent answers the user's question only from
// the lines search_documents returns on the selected matter, citing each one. It is a
// read-only chat Def: one read tool, no write tool and no gate kind, so the answer goes back
// to the screen-action route only and is never stored (AI-R-005).
func AskDef() feature.Def {
	return feature.Def{
		ID:      "F-RDY-09",
		Area:    "readiness",
		Name:    "Ask",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionAsk,
		Actions: []string{ActionAsk},
		Kind:    ai.KindChat,
		Tools:   []string{"search_documents"},
		// The run's agent principal holds search_documents' capability and no other.
		Grant:      []string{"documents.view"},
		Prompt:     promptFRDY09,
		PromptPath: "prompts/f-rdy-09.md",
		Fixture:    "testdata/ai/f-rdy-09/ask",
	}
}
