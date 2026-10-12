package access

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// EventUserRoleChanged is the a2 core event published when a user's roles change (payload
// user_id, role_ids[], gate_id). It starts F-ACC-01 and F-ACC-05.
const EventUserRoleChanged = "user.role_changed"

// Screen actions that run F-ACC-01 on request.
const (
	ActionCheckDuties = "role-editor > Check duties"
	ActionScanDuties  = "duty-rules > Scan now"
)

// readOnlyGrant is the grant of the read-only access features: search_matters' a2
// capability and nothing more.
var readOnlyGrant = []string{"matters.view"}

//go:embed prompts/f-acc-01.md
var promptFACC01 embed.FS

// DutyScanDef is F-ACC-01: daily, on a role change and on request, the agent checks the
// firm's roles and grants against its segregation-of-duty rules and answers with a breach
// list for duty-rules, each breach marked hard or soft. It is read-only: no write tool and no
// gate. A hard breach is what the GA1 role-change submit refuses on; the agent blocks
// nothing itself. The answer cites the matters search_matters returned (matter:<id>).
func DutyScanDef() feature.Def {
	return feature.Def{
		ID:         "F-ACC-01",
		Area:       "access",
		Name:       "Duty scan",
		Source:     feature.SourceJob,
		Trigger:    "schedule (daily) and on role or grant change",
		Actions:    []string{ActionCheckDuties, ActionScanDuties},
		Kind:       ai.KindChat,
		Tools:      []string{"search_matters"},
		Grant:      append([]string(nil), readOnlyGrant...),
		Prompt:     promptFACC01,
		PromptPath: "prompts/f-acc-01.md",
		Fixture:    "testdata/ai/f-acc-01/duty-scan",
	}
}

// DutyScanBinding binds F-ACC-01 to user.role_changed and its daily schedule. The scheduled
// run acts for the feature's designated lawyer in firm settings and fails closed without one;
// a role-change run acts for the event's user. a2 has no grant-change event (Q-FEAT-ACC-07).
func DutyScanBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(DutyScanDef(), EventUserRoleChanged)
}
