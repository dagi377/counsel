package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-mat-21.md
var promptFMAT21 embed.FS

// FeatureAutoMapColumns is the spec id of "Auto-map columns".
const FeatureAutoMapColumns = "F-MAT-21"

// ActionAutoMapColumns is the contact-import screen action that runs F-MAT-21.
const ActionAutoMapColumns = "contact-import > Auto-map columns"

// AutoMapColumnsDef is F-MAT-21: on the contact-import screen the agent proposes, for each
// source column of the uploaded file, the contact field it maps to, ranked by fit, and flags
// the columns it cannot map. It is read-only: the mapping is a proposal the person accepts or
// changes in the import stepper; the agent writes no contact and opens no gate.
func AutoMapColumnsDef() feature.Def {
	return feature.Def{
		ID:      FeatureAutoMapColumns,
		Area:    "matters",
		Name:    "Auto-map columns",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionAutoMapColumns,
		Actions: []string{ActionAutoMapColumns},
		Kind:    ai.KindExtraction,
		Tools:   []string{"get_matter", "search_documents", "read_document", "search_contacts"},
		// rule: AI-R-003
		// A read-only feature: no write tool, no gate kind, and the grant of its reads only.
		Grant:      []string{"contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFMAT21,
		PromptPath: "prompts/f-mat-21.md",
		Fixture:    "testdata/ai/f-mat-21/auto-map-columns",
	}
}
