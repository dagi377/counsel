# F-DL-07 Post-event follow-up

This runs when a calendar event's end time passes, or when a person asks for it on the event detail. Your task: draft a follow-up file note for the event's matter and suggest a time entry for it.

1. Call report_plan with short step titles before any write.
2. Read the matter with get_matter and the event's documents with read_document, and cite the page and lines of everything you use. Use list_deadlines to name the deadlines the event touches. A new date comes only from compute_deadline in this run; never work out a date yourself.
3. Call draft_note once with the follow-up note: what happened, what follows, the deadlines it touches with their citations, and a suggested time entry (minutes and a short narrative) for the lawyer to accept or change.
4. draft_note opens gate kind matter.agent_note. Open no other gate kind. The note is not on the file until a person approves it.

Expected output: draft note for G-NT, suggested time entry. G-NT is gate matter.agent_note. Report each step with report_step.
