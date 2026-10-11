package deadlines

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-dl-12.md
var promptFDL12 embed.FS

// ExplainStepDef is F-DL-12: in the rule-trigger wizard the agent explains the current step: which rule applies
// and why each excluded day was excluded. It is read-only: no write tool, no gate. The answer goes back through the
// screen-action route only and is never stored (AI-R-005).
func ExplainStepDef() feature.Def {
	return feature.Def{
		ID:         "F-DL-12",
		Area:       "deadlines",
		Name:       "Explain step",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: rule-trigger-wizard > Explain step",
		Actions:    []string{"rule-trigger-wizard > Explain step"},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), readTools...),
		Grant:      append([]string(nil), readGrant...),
		Prompt:     promptFDL12,
		PromptPath: "prompts/f-dl-12.md",
		Fixture:    "testdata/ai/f-dl-12/explain-step",
	}
}
