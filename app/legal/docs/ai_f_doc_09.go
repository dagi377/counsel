package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// ActionFlagOldFormVersions is the template-library action that runs F-DOC-09 on request.
const ActionFlagOldFormVersions = "template-library > Flag templates on old form versions"

//go:embed prompts/f-doc-09.md
var promptFDOC09 embed.FS

// CourtFormCurrencyDef is F-DOC-09: once a business day the agent reads the templates and the
// court forms they are built on, flags each template still on an old form version, drafts the
// template on the current version with draft_document and opens gate docs.template_publish
// (G-D2) with submit_for_approval. Nothing is republished until a lawyer approves.
func CourtFormCurrencyDef() feature.Def {
	return feature.Def{
		ID:      "F-DOC-09",
		Area:    "documents",
		Name:    "court-form-currency",
		Source:  feature.SourceJob,
		Trigger: "schedule: daily",
		Actions: []string{ActionFlagOldFormVersions},
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateTemplatePublish},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFDOC09,
		PromptPath: "prompts/f-doc-09.md",
		Fixture:    "testdata/ai/f-doc-09/court-form-currency",
	}
}

// CourtFormCurrencyBinding binds F-DOC-09 to its daily schedule. A scheduled run has no event
// user, so boot names the user it acts for (the feature's designated lawyer in firm
// settings); empty is trigger.ErrBadBinding and the feature does not run.
func CourtFormCurrencyBinding(actingFor string) (trigger.Binding, error) {
	return trigger.Bind(CourtFormCurrencyDef(), actingFor)
}
