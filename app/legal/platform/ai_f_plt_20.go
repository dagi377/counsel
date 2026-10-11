package platform

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// prepareFinalBillTrigger is how features.json words F-PLT-20's start: a screen action only.
const prepareFinalBillTrigger = "on request: matter-closing > Prepare final bill"

// ActionPrepareFinalBill is the matter-closing screen action that runs F-PLT-20.
var ActionPrepareFinalBill = strings.TrimPrefix(prepareFinalBillTrigger, "on request: ")

//go:embed prompts/f-plt-20.md
var promptFPLT20 embed.FS

// PrepareFinalBillDef is F-PLT-20: from the matter-closing screen the agent reads the matter
// being closed and its closing record, and assembles the final pre-bill of its unbilled work
// with draft_bill, which opens gate billing.prebill_review on the draft bill. The bill stays a
// draft: a lawyer other than the acting user reviews and approves it.
func PrepareFinalBillDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-20",
		Area:    "platform",
		Name:    "Prepare final bill",
		Source:  feature.SourceScreenAction,
		Trigger: prepareFinalBillTrigger,
		Actions: []string{ActionPrepareFinalBill},
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolDraftBill,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{agent.GatePrebillReview},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView, "billing.draft", agent.CapAsk},
		Prompt:     promptFPLT20,
		PromptPath: "prompts/f-plt-20.md",
		Fixture:    "testdata/ai/f-plt-20/prepare-final-bill",
	}
}
