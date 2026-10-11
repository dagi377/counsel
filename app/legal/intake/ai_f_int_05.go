package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// EventConsultationBooked is the kernel event the intake module publishes when a consultation
// is booked against a lead. It starts F-INT-05.
const EventConsultationBooked = "consultation.booked"

//go:embed prompts/f-int-05.md
var promptFINT05 embed.FS

// PreConsultBriefDef is F-INT-05: when a consultation is booked, and again the day before it,
// the agent writes the lawyer a short brief on the prospect from the contacts and matters it
// can read. It is read-only: no write tool, no gate, and the brief goes back to the caller
// only (AI-R-005).
func PreConsultBriefDef() feature.Def {
	return feature.Def{
		ID:         "F-INT-05",
		Area:       "intake",
		Name:       "Pre-consult brief",
		Source:     feature.SourceJob,
		Trigger:    "event: consultation booked; schedule: T-24h refresh",
		Kind:       ai.KindChat,
		Tools:      []string{agent.ToolSearchContacts, agent.ToolSearchMatters},
		Grant:      []string{agent.CapContactsView, agent.CapMattersView},
		Prompt:     promptFINT05,
		PromptPath: "prompts/f-int-05.md",
		Fixture:    "testdata/ai/f-int-05/pre-consult-brief",
	}
}

// PreConsultBriefBinding binds F-INT-05 to the booking event, acting for the user who booked,
// and to its daily refresh, which acts for the designated intake lawyer read from firm
// settings.
func PreConsultBriefBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(PreConsultBriefDef(), EventConsultationBooked)
}
