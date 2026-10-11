package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// EventCheckCompleted is the a2 event a conflict check publishes when its search has run. It
// starts F-CON-02.
const EventCheckCompleted = "conflict.check_completed"

//go:embed prompts/f-con-02.md
var promptFCON02 embed.FS

// SearchRankingDef is F-CON-02: after a check runs, the agent searches contacts and matters
// for the check's parties, ranks the hits by name similarity and proposes the parties of the
// check. Proposing runs the check, which leaves gate G2 (conflict.clearance) open for a lawyer
// other than the acting user; the agent never decides it.
func SearchRankingDef() feature.Def {
	return feature.Def{
		ID:      "F-CON-02",
		Area:    "conflicts",
		Name:    "Conflict search and ranking",
		Source:  feature.SourceJob,
		Trigger: "event: check run",
		Actions: []string{"conflict-results > Propose dispositions"},
		// Hits are ranked by embedding similarity of party names.
		Kind: ai.KindEmbedding,
		Tools: []string{
			"search_contacts", "search_matters", "read_document", "propose_conflict_parties",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds:  []string{GateClearance},
		Grant:      []string{"agent.ask", "conflicts.run", "contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFCON02,
		PromptPath: "prompts/f-con-02.md",
		Fixture:    "testdata/ai/f-con-02/conflict-search-and-ranking",
	}
}

// SearchRankingBinding binds F-CON-02 to the check-completed event. Event runs act for the
// event's user, so it needs no scheduled acting user.
func SearchRankingBinding() (trigger.Binding, error) {
	return trigger.Bind(SearchRankingDef(), "", EventCheckCompleted)
}
