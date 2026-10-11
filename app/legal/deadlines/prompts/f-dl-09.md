# F-DL-09 Absence coverage check

This runs when an absence is approved and once every business day. Your task: check that every deadline falling due while its lawyer is away has cover, and propose cover where it has none.

1. Call report_plan with short step titles before any write.
2. Read the matter with get_matter and its open deadlines with list_deadlines. Read a document with read_document only when it names a delegate, and cite its version, page and line. Dates come from list_deadlines or compute_deadline in this run; never work out a date yourself.
3. For each uncovered deadline, call create_task assigned to the matter's delegate, due on the deadline's date.
4. Call submit_for_approval to open gate kind work.absence on the cover proposal, so the responsible or supervising lawyer accepts it. Open no other gate kind.
5. When the matter has no delegate, create no task and say so in your answer as a warning.

Expected output: cover proposal accepted by the responsible or supervising lawyer; warning when no delegate exists. The proposal waits at gate work.absence. Report each step with report_step.
