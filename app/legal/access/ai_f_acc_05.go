package access

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// EventUserDeactivated is the a2 core event published when a user is deactivated (payload
// user_id, gate_id?). It starts F-ACC-05.
const EventUserDeactivated = "user.deactivated"

// accessEvents are the a2 events F-ACC-05 watches: the core events that change who can see
// what. a2 names no matter-access event yet (Q-FEAT-ACC-07).
var accessEvents = []string{EventUserRoleChanged, EventUserDeactivated}

//go:embed prompts/f-acc-05.md
var promptFACC05 embed.FS

// AnomalyWatchDef is F-ACC-05: on access events the agent looks for unusual access (a burst
// of matters opened, access outside a user's matters, access after deactivation) and answers
// with an alert for the owners. It is read-only: no write tool and no gate; it suspends or
// changes no one. The answer cites the matters search_matters returned (matter:<id>).
func AnomalyWatchDef() feature.Def {
	return feature.Def{
		ID:         "F-ACC-05",
		Area:       "access",
		Name:       "Anomaly watch",
		Source:     feature.SourceJob,
		Trigger:    "event (access events)",
		Kind:       ai.KindChat,
		Tools:      []string{"search_matters"},
		Grant:      append([]string(nil), readOnlyGrant...),
		Prompt:     promptFACC05,
		PromptPath: "prompts/f-acc-05.md",
		Fixture:    "testdata/ai/f-acc-05/anomaly-watch",
	}
}

// AnomalyWatchBinding binds F-ACC-05 to the access events. An event run acts for the event's
// user, so it needs no scheduled acting user.
func AnomalyWatchBinding() (trigger.Binding, error) {
	return trigger.Bind(AnomalyWatchDef(), "", accessEvents...)
}
