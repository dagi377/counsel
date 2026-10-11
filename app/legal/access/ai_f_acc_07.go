package access

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/identity"
)

// FeatureInviteListCheck is the spec id of "Invite list check".
const FeatureInviteListCheck = "F-ACC-07"

// EventInviteListUploaded is published when an admin uploads an invite list or CSV on the
// user list. a2 names no event for it (docs/QUESTIONS.md Q-FEAT-ACC-05).
const EventInviteListUploaded = "user.invite_list_uploaded"

// ActionPrecheckInviteList is the user-list screen action that runs F-ACC-07 on request.
const ActionPrecheckInviteList = "user-list > Pre-check invite list"

//go:embed prompts/f-acc-07.md
var promptFACC07 embed.FS

// InviteListCheckDef is F-ACC-07: when an invite list is uploaded, the agent extracts each
// row, checks it against the firm's records, drafts the row results with draft_document and
// submits the ready rows for gate GA2 (core.user_activation) with submit_for_approval. The
// gate's preparer is the acting user; an admin other than the preparer activates the users
// and the agent never does.
func InviteListCheckDef() feature.Def {
	return feature.Def{
		ID:      FeatureInviteListCheck,
		Area:    "access",
		Name:    "Invite list check",
		Source:  feature.SourceJob,
		Trigger: "event (list or CSV uploaded)",
		Actions: []string{ActionPrecheckInviteList},
		Kind:    ai.KindExtraction,
		Tools:   []string{"search_matters", "draft_document", "report_plan", "report_step", "submit_for_approval"},
		// rule: ARC-I-035
		// The run opens core.user_activation as a tool-gap kind: it drafts the ready rows and
		// submits the draft; it has no tool that activates a user.
		GateKinds: []string{identity.KindUserActivation},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "documents.edit", "matters.view"},
		Prompt:     promptFACC07,
		PromptPath: "prompts/f-acc-07.md",
		Fixture:    "testdata/ai/f-acc-07/invite-list-check",
	}
}

// InviteListCheckBinding binds F-ACC-07 to the invite-list upload event. Event runs act for
// the event's user, so it needs no scheduled acting user.
func InviteListCheckBinding() (trigger.Binding, error) {
	return trigger.Bind(InviteListCheckDef(), "", EventInviteListUploaded)
}
