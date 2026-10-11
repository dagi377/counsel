package matter

import (
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventCallTranscriptFiled is the kernel event a call recording or meeting transcript emits
// when it is filed to a matter with the participants' consent recorded. A recording filed
// without consent emits nothing, so F-MAT-07 never starts on it. It starts F-MAT-07.
const EventCallTranscriptFiled = "matter.call_transcript_filed"

// Matter notes and activity actions that run F-MAT-07 on request.
const (
	ActionDraftCallSummary   = "matter-notes-activity > Draft call summary note"
	ActionProposeTimeFromAct = "matter-notes-activity > Propose time entry from activity"
)

// CallSummaryBinding binds F-MAT-07 to the transcript-filed event. Event runs act for the
// event's user, so it needs no scheduled acting user.
//
// The run writes only through draft_note, which opens matter.agent_note (G-NT), and
// propose_time_entries, which opens timekeeping.accept (G9), each with the acting user as
// preparer. A person other than the preparer decides them.
func CallSummaryBinding() (trigger.Binding, error) {
	return trigger.Bind(CallSummaryDef(), "", EventCallTranscriptFiled)
}
