package billing

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureMonthlyDigest is the spec id of "Monthly billing digest".
const FeatureMonthlyDigest = "F-BIL-07"

//go:embed prompts/f-bil-07.md
var promptFBIL07 embed.FS

// MonthlyDigestDef is F-BIL-07: monthly, the agent reads unbilled time, bills and trust
// balances and answers with an internal digest for lawyers. It is read-only: it has no write
// tool and opens no gate; every citation in the answer comes from a tool result of the run.
func MonthlyDigestDef() feature.Def {
	return feature.Def{
		ID:      FeatureMonthlyDigest,
		Area:    "billing",
		Name:    "Monthly billing digest",
		Source:  feature.SourceJob,
		Trigger: "schedule monthly",
		Kind:    ai.KindChat,
		Tools:   []string{"get_matter", "list_unbilled", "get_bill", "get_trust_balance"},
		// The run's agent principal holds the read capabilities of its tools and no other.
		Grant:      []string{"matters.view", "billing.view", "trust.view"},
		Prompt:     promptFBIL07,
		PromptPath: "prompts/f-bil-07.md",
		Fixture:    "testdata/ai/f-bil-07/monthly-billing-digest",
	}
}

// MonthlyDigestBinding binds F-BIL-07 to the monthly schedule (the first business day of the
// month). The scheduled run acts for the firm's designated lawyer, read at each tick, never
// for an administrator (JOB-ACTOR).
//
// rule: ARC-I-040
func MonthlyDigestBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(MonthlyDigestDef())
}
