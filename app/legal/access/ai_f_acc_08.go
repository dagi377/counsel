package access

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionExplainRole is the roles-list screen action that runs F-ACC-08.
const ActionExplainRole = "roles-list > Explain role"

//go:embed prompts/f-acc-08.md
var promptFACC08 embed.FS

// ExplainRoleDef is F-ACC-08: on roles-list > Explain role the agent writes a plain summary
// of the selected role: what it lets a person do and what it does not. It is read-only: no
// write tool and no gate, and the screen-action route returns the answer without storing it
// (AI-R-005). The answer cites the matters search_matters returned (matter:<id>).
func ExplainRoleDef() feature.Def {
	return feature.Def{
		ID:         "F-ACC-08",
		Area:       "access",
		Name:       "Explain role",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionExplainRole,
		Actions:    []string{ActionExplainRole},
		Kind:       ai.KindChat,
		Tools:      []string{"search_matters"},
		Grant:      append([]string(nil), readOnlyGrant...),
		Prompt:     promptFACC08,
		PromptPath: "prompts/f-acc-08.md",
		Fixture:    "testdata/ai/f-acc-08/explain-role",
	}
}
