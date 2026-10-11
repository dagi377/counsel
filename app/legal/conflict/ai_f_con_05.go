package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-con-05.md
var promptFCON05 embed.FS

// ReverseSweepDef is F-CON-05: each night the agent sweeps new and changed parties against the
// open matters and, where a party now looks like a conflict, proposes it on the matter's
// conflict check with propose_conflict_parties. That reruns the check, which waits on gate
// conflict.clearance (G2); only a lawyer clears it.
func ReverseSweepDef() feature.Def {
	return feature.Def{
		ID:      "F-CON-05",
		Area:    "conflicts",
		Name:    "Reverse conflict sweep",
		Source:  feature.SourceJob,
		Trigger: "schedule: nightly",
		Kind:    ai.KindEmbedding,
		Tools: []string{
			"search_contacts", "search_matters", "read_document", "propose_conflict_parties",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateClearance},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.view", "conflicts.run", "agent.ask"},
		Prompt:     promptFCON05,
		PromptPath: "prompts/f-con-05.md",
		Fixture:    "testdata/ai/f-con-05/reverse-conflict-sweep",
	}
}

// ReverseSweepBinding binds F-CON-05 to its nightly schedule (a daily business-day cadence).
// A scheduled run has no event user, so boot names the user it acts for (the conflicts
// officer); empty is ErrBadBinding.
func ReverseSweepBinding(actingFor string) (trigger.Binding, error) {
	return trigger.Bind(ReverseSweepDef(), actingFor)
}
