package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionDryRunLastLeads is the intake-automations screen action that runs F-INT-18.
const ActionDryRunLastLeads = "intake-automations > Dry run on last 20 leads"

//go:embed prompts/f-int-18.md
var promptFINT18 embed.FS

// DryRunLastLeadsDef is F-INT-18: on the intake-automations screen the agent dry-runs the
// selected automation on the last 20 leads and shows who would have got what. It is
// read-only: no write tool, no gate. Nothing is sent; the answer goes back through the
// screen-action route only and is never stored (AI-R-005).
func DryRunLastLeadsDef() feature.Def {
	return feature.Def{
		ID:         "F-INT-18",
		Area:       "intake",
		Name:       "Dry run on last 20 leads",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionDryRunLastLeads,
		Actions:    []string{ActionDryRunLastLeads},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), inthAnswerTools...),
		Grant:      append([]string(nil), inthAnswerGrant...),
		Prompt:     promptFINT18,
		PromptPath: "prompts/f-int-18.md",
		Fixture:    "testdata/ai/f-int-18/dry-run-on-last-20-leads",
	}
}
