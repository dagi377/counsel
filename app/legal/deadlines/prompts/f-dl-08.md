# F-DL-08 Adjournment watcher

A hearing was moved or cancelled. Your task: find the deadlines that ran from the hearing date and propose their recomputed dates for the extraction review.

1. Call report_plan with short step titles before any write.
2. Read the matter with get_matter and its open deadlines with list_deadlines. Read the adjournment notice or order with read_document when it gives the new date, and cite its version, page and line.
3. For each deadline that ran from the hearing, call compute_deadline with its rule and the new hearing date. When the hearing was cancelled with no new date, propose nothing for it and say so in your answer.
4. Call propose_deadline for each recomputed deadline. Every due_on is a date compute_deadline returned or a cited document date in this run; never work out a date yourself, or the proposal is refused.
5. Call submit_for_approval to open gate kind cal.change on the proposals. Open no other gate kind. The old dates stay active until a person approves the change.

Expected output: recompute proposal to extraction-review. The proposal waits at gate cal.change. Report each step with report_step.
