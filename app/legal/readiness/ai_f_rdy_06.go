package readiness

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionVoiceNoteToTimeEntry is the mobile-app action that runs F-RDY-06 on request.
const ActionVoiceNoteToTimeEntry = "mobile-app > Voice note to time entry"

//go:embed prompts/f-rdy-06.md
var promptFRDY06 embed.FS

// VoiceNoteToTimeEntryDef is F-RDY-06: on request from the mobile app, the agent finds the
// transcript of the acting user's voice note among the matter's documents with
// search_documents and proposes the time entry it describes with propose_time_entries. The
// entry is saved as suggested and waits at gate timekeeping.accept for the acting user; the
// agent never accepts it. Its work date and minutes must be values the transcript states.
func VoiceNoteToTimeEntryDef() feature.Def {
	return feature.Def{
		ID:      "F-RDY-06",
		Area:    "readiness",
		Name:    "Voice note to time entry",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionVoiceNoteToTimeEntry,
		Actions: []string{ActionVoiceNoteToTimeEntry},
		// app/legal/agent is not imported here, so its tool names are spelled out.
		Tools: []string{"search_documents", "propose_time_entries", "report_plan", "report_step", "submit_for_approval"},
		// rule: ARC-I-035
		// agent.GateTimeAccept (G9): propose_time_entries opens it on the suggested entry.
		GateKinds: []string{"timekeeping.accept"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"documents.view", "time.record", "agent.ask"},
		Prompt:     promptFRDY06,
		PromptPath: "prompts/f-rdy-06.md",
		Fixture:    "testdata/ai/f-rdy-06/voice-note-to-time-entry",
	}
}
