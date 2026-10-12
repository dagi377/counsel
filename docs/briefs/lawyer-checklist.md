<!-- Copied from the build session's scratchpad so the path survives; paths are generalised. -->

# Counsel: points for a licensed Ontario lawyer to confirm (all are flagged (verify) in code)
- Limitations Act s.22(4)/(5): an agreement can extend the 15-year period only after discovery. Business agreements fall under s.22(5) para 2. Code ignores agreements made before discovery.
- Legislation Act s.87/s.88: a limitation period ending on a Sunday or listed holiday rolls forward; Saturday does not. Confirm the closed-office rule and the r.1.03 substitute Mondays.
- ETA s.23 / s.7(b): non-resident zero-rating. The litigation exclusion applies to individuals only, per frozen rule BIL-T-015. Check against GST/HST Memorandum 4.5.3.
- Solicitors Act s.33 vs CJA s.127: the prejudgment-rate default sits behind interest.cja_default_rate (off).
- Sep 30 (Truth and Reconciliation) is not treated as a court holiday.
- The cooling-off period on trust actions is 24h of elapsed time (a product control, not a By-law 9 rule).
- The BN check is on by default, based on the ITC Information Regulations.
- Write-off second approval: write-offs are aggregated per bill and per client within 30 days.
- Place of supply (T-014.2): the full 49-row table, with uncertain rows behind tax.verify_rules.
- Trust withdrawal authority expires 72h after approval (product control; confirm it suits firm practice).
- [ ] Sole-practitioner self-sign of the monthly trust reconciliation (Q-PORT-24): confirm this satisfies By-law 9 s.22 sign-off.
- [ ] Trust overdraft display: the API reports the signed balance plus overdrawn/shortfall; confirm the overdraft reporting duty and wording (By-law 9).
- [ ] Write-off after a trust fee transfer: a pro-rata split of the unpaid balance across fees, disbursements and HST (Q-INTEG-12.1, ETA s.231).
- [ ] A fee transfer from trust to the firm general account is allowed only with a delivered bill and amount ≤ the unpaid balance (By-law 9 s.9(1)); confirm.
- [ ] Trust receipts: recorded the same day as received_pending_classification and posted to the client ledger only after the classification gate (Q-HTTP-16). Confirm this meets the By-law 9 s.18 'record when received' duty.
- [ ] Trust drafts: every cents figure in an AI trust draft or email must match a value read in the run (SourcedDraft). Confirm this is sufficient for LSO annual report and spot-audit figures.
- [ ] Trust reconciliation sign-off package = SHA-256 of the reconciliation data; confirm this is an adequate tamper-evident record of the signed monthly reconciliation.
- [ ] Sole-practitioner self-approval of money and conflict steps (step-up, reason, audit), and a delayed second self-confirmation for two-approver kinds.
- [ ] Cooling-off override with a documented reason for same-day closings.
- [ ] Limitation computation uses the Legislation Act holiday definition (check s.87/s.88 against the weekend-substitute Monday issue).
- [ ] The firm may be the payee for disbursement reimbursement on a delivered bill (By-law 9 s.9(1)(b)).
