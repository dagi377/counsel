package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/matter"
)

// Screen actions of matter-closing that run F-PLT-11 on request.
const (
	ActionRunClosingChecklist = "matter-closing > Run closing checklist"
	ActionDraftClosingLetter  = "matter-closing > Draft closing letter"
)

//go:embed prompts/f-plt-11.md
var promptFPLT11 embed.FS

// StaleMatterSweepDef is F-PLT-11: each week, or on request from matter-closing, the agent
// finds open matters with no activity whose work looks done, reads each one's file, drafts
// its closing checklist (or closing letter) on that matter with draft_document and opens gate
// platform.closing (G13, matter.GateClosing), a tool-gap kind, with submit_for_approval. A
// lawyer decides; the run closes no matter.
func StaleMatterSweepDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-11",
		Area:    "platform",
		Name:    "Stale matter sweep",
		Source:  feature.SourceJob,
		Trigger: "schedule (weekly)",
		Actions: []string{ActionRunClosingChecklist, ActionDraftClosingLetter},
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolDraftDocument,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{matter.GateClosing},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView, "documents.edit", agent.CapAsk},
		Prompt:     promptFPLT11,
		PromptPath: "prompts/f-plt-11.md",
		Fixture:    "testdata/ai/f-plt-11/stale-matter-sweep",
	}
}

// StaleMatterSweepBinding binds F-PLT-11 to its weekly schedule. A scheduled run has no event
// user, so it acts for the feature's designated lawyer in firm settings, read at each tick
// (trigger.BindDesignated); with none named the feature is unready and does not run.
func StaleMatterSweepBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(StaleMatterSweepDef())
}
