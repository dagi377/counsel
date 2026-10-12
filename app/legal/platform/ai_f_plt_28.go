package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionProposeTemplate is the email-settings action that runs F-PLT-28.
const ActionProposeTemplate = "email-settings > Propose template"

//go:embed prompts/f-plt-28.md
var promptFPLT28 embed.FS

// ProposeTemplateDef is F-PLT-28: on request from the email-settings screen, the agent reads
// the firm's past correspondence and proposes an email template, drafted on the firm with
// draft_document (subject_ref "firm": email settings are the firm's, not one matter's), and
// opens gate platform.skill_autonomy (G-SK), a tool-gap kind, with submit_for_approval. The
// template is used only after a person other than the acting user approves it.
func ProposeTemplateDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-28",
		Area:    "platform",
		Name:    "Propose template",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionProposeTemplate,
		Actions: []string{ActionProposeTemplate},
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolDraftDocument,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{GateSkillAutonomy},
		// The run's agent principal holds the capabilities of its tools, and the firm-draft
		// capability draft_document checks for a draft on the firm subject (Q-FORM-SUBJECT).
		Grant: []string{
			agent.CapMattersView, agent.CapDocumentsView, "documents.edit", agent.CapAsk, agent.CapIntakeForms,
		},
		Prompt:     promptFPLT28,
		PromptPath: "prompts/f-plt-28.md",
		Fixture:    "testdata/ai/f-plt-28/propose-template",
	}
}
