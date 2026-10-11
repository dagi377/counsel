package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionSuggestOrderAndTabs is the bundle-builder action that runs F-DOC-22 on request.
const ActionSuggestOrderAndTabs = "bundle-builder > Suggest order and tabs"

//go:embed prompts/f-doc-22.md
var promptFDOC22 embed.FS

// SuggestOrderAndTabsDef is F-DOC-22: on the bundle-builder screen the agent reads the
// selected documents and suggests their order in the bundle and a tab for each. The user
// orders the bundle; the run stores nothing.
func SuggestOrderAndTabsDef() feature.Def {
	return screenAnswerDef("F-DOC-22", "Suggest order and tabs", ActionSuggestOrderAndTabs,
		promptFDOC22, "prompts/f-doc-22.md", "testdata/ai/f-doc-22/suggest-order-and-tabs")
}
