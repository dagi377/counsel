package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionWeeklyRiskDigest is the reports action that runs F-PLT-05 on request.
const ActionWeeklyRiskDigest = "reports > Weekly risk digest"

//go:embed prompts/f-plt-05.md
var promptFPLT05 embed.FS

// RiskDigestDef is F-PLT-05: each week, or when a lawyer asks on the reports screen, the agent
// reads the firm's matters and their documents and answers with an internal risk digest, every
// point cited to the version, page and line it rests on. It is read-only: no write tool and no
// gate; the digest goes back to the caller and is never stored (AI-R-005).
func RiskDigestDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-05",
		Area:    "platform",
		Name:    "Risk digest",
		Source:  feature.SourceJob,
		Trigger: "schedule: weekly",
		Actions: []string{ActionWeeklyRiskDigest},
		Kind:    ai.KindChat,
		Tools:   []string{agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument},
		// rule: AI-R-003
		// A read-only feature: no write tool, no gate kind, and the grant of its reads only.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView},
		Prompt:     promptFPLT05,
		PromptPath: "prompts/f-plt-05.md",
		Fixture:    "testdata/ai/f-plt-05/risk-digest",
	}
}

// RiskDigestBinding binds F-PLT-05 to its weekly schedule (the first business day of the
// week). A scheduled run has no event user, so it acts for the feature's designated lawyer in
// firm settings, read at each tick (trigger.BindDesignated); with none named the feature is
// unready and does not run. The on-request start is the Def's screen action.
func RiskDigestBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(RiskDigestDef())
}
