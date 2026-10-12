package platform

// F-PLT-04 write path and trigger.
//
// A run writes only through three tools, each opening a gate with the acting user as
// preparer through the run's scoped opener (ARC-I-035):
//
//   - draft_document stores the summary and the follow-up tasks as one draft for the acting
//     user; submit_for_approval then opens G-SUM (platform.summary_accuracy) on that draft.
//     G-SUM is a tool-gap kind with no dedicated a2 write tool (Q-FEAT-PLT-06).
//   - propose_time_entries opens G9 (timekeeping.accept) on the call's time. Its work date and
//     minutes must be values the transcript or a read of the run stated (ARC-U-027).
//
// A person other than the preparer decides each gate; the agent never does.
//
// F-PLT-04 binds no event. F-MAT-07 owns matter.call_transcript_filed, so one transcript
// starts one summary; F-PLT-04 runs on request from the comms-log only (boot lists it as an
// on-request job feature).

// ActionSummarizeCall is the comms-log action that runs F-PLT-04 on the selected call.
const ActionSummarizeCall = "comms-log > Summarize call"

// CallSummarySubjectType is the submit_for_approval subject type of the summary draft: the
// document draft_document stored.
const CallSummarySubjectType = "document"
