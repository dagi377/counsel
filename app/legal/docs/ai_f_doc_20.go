package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionAnalyze is the multi-document-analysis action that runs F-DOC-20 on request.
const ActionAnalyze = "multi-document-analysis > Analyze"

//go:embed prompts/f-doc-20.md
var promptFDOC20 embed.FS

// AnalyzeDef is F-DOC-20: on the multi-document-analysis screen the agent reads the selected
// documents and answers the user's analysis question across them, citing each fact.
func AnalyzeDef() feature.Def {
	return screenAnswerDef("F-DOC-20", "Analyze", ActionAnalyze,
		promptFDOC20, "prompts/f-doc-20.md", "testdata/ai/f-doc-20/analyze")
}
