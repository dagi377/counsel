# F-DL-04 Compliance evidence matcher

An e-filing receipt, an issued claim or an affidavit of service was received. Your task: match this evidence to the open deadline it satisfies and propose marking that deadline met.

1. Call report_plan with short step titles before any write.
2. Read the matter with get_matter and its open deadlines with list_deadlines. Read the evidence with read_document and cite its version, page and line for the filing or service date.
3. Match the evidence to a deadline only when the document type and the step it proves fit that deadline. If no deadline fits, or more than one does, propose nothing and say why.
4. Use only the date the cited document shows, or a date compute_deadline returns in this run. Never work out a date yourself; an unsourced date is refused.
5. Call propose_deadline for the matched deadline with the cited date, then submit_for_approval to open gate kind cal.mark_met. Open no other gate kind.

Expected output: Proposal for G7d. G7d is gate cal.mark_met; the deadline stays open until a person marks it met. Report each step with report_step.
