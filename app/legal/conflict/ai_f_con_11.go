package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionWeeklyAnomalyDigest is the compliance-audit-log screen action that runs F-CON-11 on
// request.
const ActionWeeklyAnomalyDigest = "compliance-audit-log > Weekly anomaly digest"

// readOnlyTools are the three read tools the read-only conflict features call, and readOnlyGrant the
// capabilities their runs hold: each tool's a2 capability and nothing more.
var (
	readOnlyTools = []string{"search_contacts", "search_matters", "read_document"}
	readOnlyGrant = []string{"contacts.view", "matters.view", "documents.view"}
)

//go:embed prompts/f-con-11.md
var promptFCON11 embed.FS

// AuditAnomalyDigestDef is F-CON-11: each week the agent reads the conflict and identification
// records it can see and writes a digest of anomalies for the admin and the lawyer. It is
// read-only: no write tool, no gate, and the digest goes back to the caller only (AI-R-005).
func AuditAnomalyDigestDef() feature.Def {
	return feature.Def{
		ID:         "F-CON-11",
		Area:       "conflicts",
		Name:       "Audit anomaly digest",
		Source:     feature.SourceJob,
		Trigger:    "schedule: weekly",
		Actions:    []string{ActionWeeklyAnomalyDigest},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), readOnlyTools...),
		Grant:      append([]string(nil), readOnlyGrant...),
		Prompt:     promptFCON11,
		PromptPath: "prompts/f-con-11.md",
		Fixture:    "testdata/ai/f-con-11/audit-anomaly-digest",
	}
}

// AuditAnomalyDigestBinding binds F-CON-11 to its weekly schedule (the first business day of
// the week). A scheduled run has no event user, so boot names the user it acts for; empty is
// trigger.ErrBadBinding.
func AuditAnomalyDigestBinding(actingFor string) (trigger.Binding, error) {
	return trigger.Bind(AuditAnomalyDigestDef(), actingFor)
}
