package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-plt-09.md
var promptFPLT09 embed.FS

// InsightsNoteDef is F-PLT-09: at each month end the agent reads the firm's matters and their
// documents and answers with a short insights note for the firm-insights screen, every point
// cited to the version, page and line it rests on. It is read-only: no write tool and no gate;
// the note goes back to the caller and is never stored (AI-R-005).
func InsightsNoteDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-09",
		Area:    "platform",
		Name:    "Insights note",
		Source:  feature.SourceJob,
		Trigger: "schedule (month end)",
		Kind:    ai.KindChat,
		Tools:   []string{agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument},
		// rule: AI-R-003
		// A read-only feature: no write tool, no gate kind, and the grant of its reads only.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView},
		Prompt:     promptFPLT09,
		PromptPath: "prompts/f-plt-09.md",
		Fixture:    "testdata/ai/f-plt-09/insights-note",
	}
}

// InsightsNoteBinding binds F-PLT-09 to the month end. The spec's "schedule (month end)" has
// no cadence word, which trigger.ParseCadence reads as daily; the note covers the month just
// closed, so it runs monthly (the first business day of the month) instead (Q-FEAT-PLT-23). A
// scheduled run has no event user, so it acts for the feature's designated lawyer in firm
// settings, read at each tick (trigger.BindDesignated); with none named it does not run.
func InsightsNoteBinding() (trigger.Binding, error) {
	b, err := trigger.BindDesignated(InsightsNoteDef())
	if err != nil {
		return trigger.Binding{}, err
	}
	b.Cadence = trigger.CadenceMonthly
	return b, nil
}
