package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// Events that start F-DOC-10: the matter module publishes them when a matter opens and when
// it moves to another stage.
const (
	EventMatterOpened       = "matter.opened"
	EventMatterStageChanged = "matter.stage_changed"
)

// Document-assembly actions that run F-DOC-10 on request.
const (
	ActionAssemblyPrefill  = "document-assembly > Prefill"
	ActionAssemblyGenerate = "document-assembly > Generate"
)

//go:embed prompts/f-doc-10.md
var promptFDOC10 embed.FS

// AssemblyOnStageDef is F-DOC-10: when a matter opens or changes stage, the agent reads the
// matter and its documents, assembles the documents the stage needs with draft_document and
// opens gate docs.outgoing (G8) for each with submit_for_approval. When the documents disagree
// on a fact, it drafts nothing and reports the conflict.
func AssemblyOnStageDef() feature.Def {
	return feature.Def{
		ID:      "F-DOC-10",
		Area:    "documents",
		Name:    "assembly-on-stage",
		Source:  feature.SourceJob,
		Trigger: "event: matter opened or stage changed",
		Actions: []string{ActionAssemblyPrefill, ActionAssemblyGenerate},
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateOutgoing},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFDOC10,
		PromptPath: "prompts/f-doc-10.md",
		Fixture:    "testdata/ai/f-doc-10/assembly-on-stage",
	}
}

// AssemblyOnStageBinding binds F-DOC-10 to the matter-opened and stage-changed events. Event
// runs act for the event's user, so it needs no scheduled acting user.
func AssemblyOnStageBinding() (trigger.Binding, error) {
	return trigger.Bind(AssemblyOnStageDef(), "", EventMatterOpened, EventMatterStageChanged)
}
