package access

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/identity"
)

// EventUserInvited is the kernel event the identity module publishes when an administrator
// invites a user. It starts F-ACC-02.
const EventUserInvited = "user.invited"

//go:embed prompts/f-acc-02.md
var promptFACC02 embed.FS

// LicenceCheckDef is F-ACC-02: on an invite, and monthly, the agent checks the firm's seats
// against its licence, drafts the warning and the downgrade it proposes with draft_document,
// and opens gate core.user_activation (GA2), a tool-gap kind, with submit_for_approval. No
// user is activated or downgraded until an administrator approves.
func LicenceCheckDef() feature.Def {
	return feature.Def{
		ID:      "F-ACC-02",
		Area:    "access",
		Name:    "Licence check",
		Source:  feature.SourceJob,
		Trigger: "on invite and monthly",
		Tools: []string{
			"search_matters", "draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{identity.KindUserActivation},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.edit", "agent.ask"},
		Prompt:     promptFACC02,
		PromptPath: "prompts/f-acc-02.md",
		Fixture:    "testdata/ai/f-acc-02/licence-check",
	}
}

// LicenceCheckBinding binds F-ACC-02 to the invite event and the monthly schedule. A
// scheduled run acts for the feature's designated lawyer in firm settings and fails closed
// without one; an invite run acts for the inviting user.
func LicenceCheckBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(LicenceCheckDef(), EventUserInvited)
}
