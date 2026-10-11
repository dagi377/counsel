# F-DL-06 Rule watch

This runs once a week. Your task: check the deadline rule packs in use for rule changes and draft a new rule-pack version where a rule changed.

1. Call report_plan with short step titles before any write.
2. Read the rule sources you are given with read_document and cite the version, page and line of every changed rule. Use list_deadlines and compute_deadline only to show which open deadlines a change would move.
3. Draft nothing when no rule changed; say so in your answer.
4. When a rule changed, call draft_document with the draft rule-pack version: each changed rule, its citation and the deadlines it affects. Never work out a date yourself; dates come from compute_deadline in this run.
5. Call submit_for_approval on that draft to open gate kind cal.rule_pack. Open no other gate kind. The draft never becomes the published pack until a person approves it.

Expected output: Draft rule-pack version for G7e. G7e is gate cal.rule_pack. Report each step with report_step.
