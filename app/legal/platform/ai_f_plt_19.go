package platform

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// GateSkillAutonomy is the gate kind of spec gate G-SK: a person with the agent-settings
// capability changes how far a skill may act on its own, and a template or skill the agent
// proposes (F-PLT-28) takes effect only after such a person approves it. It is the kind the
// intake features open too.
const GateSkillAutonomy = "platform.skill_autonomy"

// recommendSettingsTrigger is how features.json words F-PLT-19's start: a screen action only.
const recommendSettingsTrigger = "on request: automation-settings > Recommend settings"

// ActionRecommendSettings is the automation-settings screen action that runs F-PLT-19.
var ActionRecommendSettings = strings.TrimPrefix(recommendSettingsTrigger, "on request: ")

//go:embed prompts/f-plt-19.md
var promptFPLT19 embed.FS

// RecommendSettingsDef is F-PLT-19: from the automation-settings screen the agent reads the
// correction history filed on the selected matter, drafts its recommended automation settings
// on the firm with draft_document (subject_ref "firm": settings belong to the firm, not to the
// matter it read) and opens gate platform.skill_autonomy (G-SK), a tool-gap kind, with
// submit_for_approval. People decide; the run changes no setting.
func RecommendSettingsDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-19",
		Area:    "platform",
		Name:    "Recommend settings",
		Source:  feature.SourceScreenAction,
		Trigger: recommendSettingsTrigger,
		Actions: []string{ActionRecommendSettings},
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
		Prompt:     promptFPLT19,
		PromptPath: "prompts/f-plt-19.md",
		Fixture:    "testdata/ai/f-plt-19/recommend-settings",
	}
}
