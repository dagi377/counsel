package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

//go:embed prompts/f-plt-15.md
var promptFPLT15 embed.FS

// RetainerCoverageDef is F-PLT-15: each business day the agent reads a matter's trust ledger
// statement and its work in progress, and where the retainer no longer covers the work ahead
// it drafts a replenishment request to the client with draft_email, submitted, which opens
// platform.outgoing_email (G-OUT) with the acting user as preparer. It never moves trust
// money, never approves and never hides an overdraft: a negative balance is reported to the
// lawyer, not drafted around. Nothing is sent until a person other than the preparer approves.
func RetainerCoverageDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-15",
		Area:    "platform",
		Name:    "Retainer coverage watch",
		Source:  feature.SourceJob,
		Trigger: "schedule: daily",
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolDraftEmail,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		// G-OUT is opened only by draft_email with submit, through the run's scoped opener.
		GateKinds: []string{agent.GateOutgoingEmail},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView, "documents.edit", agent.CapAsk},
		Prompt:     promptFPLT15,
		PromptPath: "prompts/f-plt-15.md",
		Fixture:    "testdata/ai/f-plt-15/retainer-coverage-watch",
	}
}

// RetainerCoverageBinding binds F-PLT-15 to its daily schedule. A scheduled run has no event
// user, so it acts for the feature's designated lawyer in firm settings, read at each tick
// (trigger.BindDesignated); with none named the feature is unready and does not run.
func RetainerCoverageBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(RetainerCoverageDef())
}
