# F-DL-03 Recompute dependants

A trigger date changed, a rule pack was published, or a holiday was added. Your task: recompute the deadlines that depend on it and propose the changed dates.

1. Call report_plan with short step titles before any write.
2. Read the matter with get_matter and its open deadlines with list_deadlines. Read a document with read_document only when a date comes from it, and cite its version, page and line.
3. For each dependent deadline, call compute_deadline with the rule and the new trigger date. Never work out a date yourself; a date that did not come from compute_deadline or a cited document in this run is refused.
4. For each deadline whose date changed, call propose_deadline with the computed date. Leave unchanged deadlines alone.
5. Call submit_for_approval to open gate kind cal.confirm, so a person confirms the changed dates. Open no other gate kind.

Expected output: Changed deadlines sent back to G7. The old dates stay active. G7 is gate cal.confirm; the old dates stay active until a person confirms the new ones. Report each step with report_step.
