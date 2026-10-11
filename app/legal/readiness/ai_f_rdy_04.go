package readiness

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// insurerAnswersTrigger is how features.json words F-RDY-04's start: a screen action only.
const insurerAnswersTrigger = "on request: trust-center > Draft insurer questionnaire answers"

// ActionDraftInsurerAnswers is the trust-center action that runs F-RDY-04.
var ActionDraftInsurerAnswers = strings.TrimPrefix(insurerAnswersTrigger, "on request: ")

//go:embed prompts/f-rdy-04.md
var promptFRDY04 embed.FS

// InsurerAnswersDef is F-RDY-04: from trust-center the agent answers an insurer's security
// questionnaire from the firm's controls list (search_documents on the selected readiness
// matter), drafts the answers with draft_document and opens gate platform.security_answer
// (G-SEC), a tool-gap kind, with submit_for_approval. The owner reviews before anything is
// sent; the run sends nothing.
func InsurerAnswersDef() feature.Def {
	return feature.Def{
		ID:      "F-RDY-04",
		Area:    "readiness",
		Name:    "Draft insurer questionnaire answers",
		Source:  feature.SourceScreenAction,
		Trigger: insurerAnswersTrigger,
		// The screen action is the trigger itself.
		Actions: []string{ActionDraftInsurerAnswers},
		Tools: []string{
			agent.ToolSearchDocuments, agent.ToolDraftDocument,
			agent.ToolReportPlan, agent.ToolReportStep, agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{GateSecurityAnswer},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapDocumentsView, "documents.edit", agent.CapAsk},
		Prompt:     promptFRDY04,
		PromptPath: "prompts/f-rdy-04.md",
		Fixture:    "testdata/ai/f-rdy-04/draft-insurer-questionnaire-answers",
	}
}
