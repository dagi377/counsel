package access

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionExplainAccess is the user-detail screen action that runs F-ACC-09.
const ActionExplainAccess = "user-detail > Explain access"

//go:embed prompts/f-acc-09.md
var promptFACC09 embed.FS

// ExplainAccessDef is F-ACC-09: on user-detail > Explain access the agent answers why the
// selected person can (or cannot) see a matter: their role, their matter team, a wall. It is
// read-only: no write tool and no gate, and the screen-action route returns the answer
// without storing it (AI-R-005). search_matters applies the acting user's walls and returns no
// document, so the answer cites none (Uncited).
func ExplainAccessDef() feature.Def {
	return feature.Def{
		ID:         "F-ACC-09",
		Area:       "access",
		Name:       "Explain access",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionExplainAccess,
		Actions:    []string{ActionExplainAccess},
		Kind:       ai.KindChat,
		Tools:      []string{"search_matters"},
		Grant:      append([]string(nil), readOnlyGrant...),
		Uncited:    true,
		Prompt:     promptFACC09,
		PromptPath: "prompts/f-acc-09.md",
		Fixture:    "testdata/ai/f-acc-09/explain-access",
	}
}
