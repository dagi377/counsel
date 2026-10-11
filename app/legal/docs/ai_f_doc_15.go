package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionProposeFields is the template-library action that runs F-DOC-15 on request.
const ActionProposeFields = "template-library > Propose fields"

//go:embed prompts/f-doc-15.md
var promptFDOC15 embed.FS

// ProposeFieldsDef is F-DOC-15: on request from the template library, the agent reads the
// imported file, finds its blanks, sample names and dates, drafts the template with merge
// fields in their place with draft_document and opens gate docs.template_publish with
// submit_for_approval. Nothing is published until a person approves.
func ProposeFieldsDef() feature.Def {
	return feature.Def{
		ID:      "F-DOC-15",
		Area:    "documents",
		Name:    "Propose fields",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionProposeFields,
		Actions: []string{ActionProposeFields},
		Kind:    ai.KindExtraction,
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateTemplatePublish},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFDOC15,
		PromptPath: "prompts/f-doc-15.md",
		Fixture:    "testdata/ai/f-doc-15/propose-fields",
	}
}
