package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionAnonymize is the precedent-library action that runs F-DOC-17 on request.
const ActionAnonymize = "precedent-library > Anonymize"

//go:embed prompts/f-doc-17.md
var promptFDOC17 embed.FS

// AnonymizeDef is F-DOC-17: on request from the precedent library, the agent reads the
// selected document, drafts an anonymized copy with names, addresses, amounts and identifying
// facts replaced, plus a list of what changed, with draft_document and opens gate
// docs.precedent with submit_for_approval. The copy joins the library only after a person
// approves it.
func AnonymizeDef() feature.Def {
	return feature.Def{
		ID:      "F-DOC-17",
		Area:    "documents",
		Name:    "Anonymize",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionAnonymize,
		Actions: []string{ActionAnonymize},
		Kind:    ai.KindExtraction,
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GatePrecedent},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFDOC17,
		PromptPath: "prompts/f-doc-17.md",
		Fixture:    "testdata/ai/f-doc-17/anonymize",
	}
}
