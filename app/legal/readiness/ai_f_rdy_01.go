// Package readiness holds the AI feature definitions of the firm's launch-readiness screens
// (trust-center, research, provinces-languages). Each Def is listed in
// app/legal/agent/featureset and bound at boot; nothing registers from init().
package readiness

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventQuestionnaireReceived is the event trust-center emits when a client or insurer
// security questionnaire is filed for answering, with the matter that holds it and the firm's
// controls list, and the filing user as user_id. It starts F-RDY-01.
const EventQuestionnaireReceived = "readiness.questionnaire_received"

// GateSecurityAnswer is the tool-gap gate kind of a drafted questionnaire answer set (G-SEC).
const GateSecurityAnswer = "platform.security_answer"

//go:embed prompts/f-rdy-01.md
var promptFRDY01 embed.FS

// QuestionnaireFillerDef is F-RDY-01: when a security questionnaire is filed, the agent finds
// the controls that answer each question with search_documents, drafts the answers on that
// matter with draft_document and opens gate platform.security_answer, a tool-gap kind, with
// submit_for_approval. A person other than the acting user approves the answers; the run
// sends nothing.
func QuestionnaireFillerDef() feature.Def {
	return feature.Def{
		ID:      "F-RDY-01",
		Area:    "readiness",
		Name:    "Questionnaire filler",
		Source:  feature.SourceJob,
		Trigger: "event",
		Tools: []string{
			agent.ToolSearchDocuments, agent.ToolDraftDocument,
			agent.ToolReportPlan, agent.ToolReportStep, agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{GateSecurityAnswer},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapDocumentsView, "documents.edit", agent.CapAsk},
		Prompt:     promptFRDY01,
		PromptPath: "prompts/f-rdy-01.md",
		Fixture:    "testdata/ai/f-rdy-01/questionnaire-filler",
	}
}

// QuestionnaireFillerBinding binds F-RDY-01 to a filed questionnaire. The run acts for the
// user who filed it (the event's user), so it has no schedule.
func QuestionnaireFillerBinding() (trigger.Binding, error) {
	return trigger.Bind(QuestionnaireFillerDef(), "", EventQuestionnaireReceived)
}
