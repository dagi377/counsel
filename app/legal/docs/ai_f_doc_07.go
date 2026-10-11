package docs

import (
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeaturePrecedentCandidates is the spec id of "precedent-candidates".
const FeaturePrecedentCandidates = "F-DOC-07"

// PrecedentCandidatesDef is F-DOC-07: once a month the agent reads the firm's recent work
// product and proposes documents worth keeping as precedents. Each proposal is drafted with
// draft_document and submitted to docs.precedent (G-D3), with the acting user as preparer; a
// person with the gate capability decides whether it joins the precedent library.
func PrecedentCandidatesDef() feature.Def {
	return feature.Def{
		ID:      FeaturePrecedentCandidates,
		Area:    "documents",
		Name:    "precedent-candidates",
		Source:  feature.SourceJob,
		Trigger: "schedule: monthly",
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// docs.precedent has no dedicated write tool: the run drafts the proposal and submits it.
		GateKinds:  []string{GatePrecedent},
		Grant:      []string{"agent.ask", "documents.edit", "documents.view", "matters.view"},
		Prompt:     prompts,
		PromptPath: "prompts/f-doc-07.md",
		Fixture:    "testdata/ai/f-doc-07/precedent-candidates",
	}
}

// PrecedentCandidatesBinding binds F-DOC-07 to its monthly schedule. A scheduled run acts for
// the feature's designated lawyer in firm settings and does not start while none is named.
func PrecedentCandidatesBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(PrecedentCandidatesDef())
}
