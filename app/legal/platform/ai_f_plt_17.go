package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-plt-17.md
var promptFPLT17 embed.FS

// FeaturePrepareSignatureFields is the spec id of "Prepare signature fields".
const FeaturePrepareSignatureFields = "F-PLT-17"

// ActionPrepareSignatureFields is the esign-request action that runs F-PLT-17 on the
// selected document.
const ActionPrepareSignatureFields = "esign-request > Prepare signature fields"

// PrepareSignatureFieldsDef is F-PLT-17: on the esign-request screen the agent reads the
// document to be signed and lists the signature, initial and date blocks it finds, ranked
// by page and line, each with a citation, and flags blocks it cannot place. It is read-only:
// the list is a proposal the person places or changes before sending; the agent writes no
// field, sends nothing and opens no gate. Sending stays the human "Send for signature" step.
func PrepareSignatureFieldsDef() feature.Def {
	return feature.Def{
		ID:      FeaturePrepareSignatureFields,
		Area:    "platform",
		Name:    "Prepare signature fields",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionPrepareSignatureFields,
		Actions: []string{ActionPrepareSignatureFields},
		Kind:    ai.KindExtraction,
		Tools:   []string{agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument},
		// rule: AI-R-003
		// A read-only feature: no write tool, no gate kind, and the grant of its reads only.
		Grant:      []string{agent.CapDocumentsView, agent.CapMattersView},
		Prompt:     promptFPLT17,
		PromptPath: "prompts/f-plt-17.md",
		Fixture:    "testdata/ai/f-plt-17/prepare-signature-fields",
		// rule: ARC-I-038
		// The extraction is an {answer, citations} answer, checked with run.CheckAnswer.
		OutputSchema: run.AnswerSchema,
	}
}
