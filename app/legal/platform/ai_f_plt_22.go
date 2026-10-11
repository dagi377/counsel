package platform

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/matter"
)

// suggestPracticeAreaTrigger is how features.json words F-PLT-22's start: a screen action
// only.
const suggestPracticeAreaTrigger = "on request: firm-profile > Suggest practice area setup"

// ActionSuggestPracticeArea is the firm-profile screen action that runs F-PLT-22.
var ActionSuggestPracticeArea = strings.TrimPrefix(suggestPracticeAreaTrigger, "on request: ")

//go:embed prompts/f-plt-22.md
var promptFPLT22 embed.FS

// SuggestPracticeAreaDef is F-PLT-22: from the firm-profile screen the agent reads the
// selected matter's practice notes, drafts the stages, checklist and retention of a new
// practice area on the firm with draft_document (subject_ref "firm": a practice area is the
// firm's configuration, not the matter's) and opens gate matter.practice_config (G-CF), a
// tool-gap kind, with submit_for_approval. People decide; the run configures nothing.
func SuggestPracticeAreaDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-22",
		Area:    "platform",
		Name:    "Suggest practice area setup",
		Source:  feature.SourceScreenAction,
		Trigger: suggestPracticeAreaTrigger,
		Actions: []string{ActionSuggestPracticeArea},
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolDraftDocument,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{matter.GatePracticeConfig},
		// The run's agent principal holds the capabilities of its tools, and the firm-draft
		// capability draft_document checks for a draft on the firm subject (Q-FORM-SUBJECT).
		Grant: []string{
			agent.CapMattersView, agent.CapDocumentsView, "documents.edit", agent.CapAsk, agent.CapIntakeForms,
		},
		Prompt:     promptFPLT22,
		PromptPath: "prompts/f-plt-22.md",
		Fixture:    "testdata/ai/f-plt-22/suggest-practice-area-setup",
	}
}
