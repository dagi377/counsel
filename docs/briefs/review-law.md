<!-- Copied from the build session's scratchpad so the path survives; paths are generalised. -->

# Counsel: Ontario legal audit of origin/main 4c45ac3b

I reviewed this as an Ontario lawyer and legal-tech auditor. The review was read-only, done in a detached worktree. Paths are relative to the repo root.

This review does not replace a licensed lawyer's sign-off. Every point marked "(verify)" needs the statute or by-law text checked before the related flag is turned on. The legal premises behind B1–B3 are firm. M7, and the Legislation Act part of M8, are from memory and need checking.

**Ranks.** A **blocker** stops a real Ontario firm from using the product lawfully, or creates a malpractice or By-law 9 breach on day one. A **major** is a wrong rule or a missing mandatory record. A **minor** is a gap in hardening or wording.

## Summary

| # | Rank | Area | Finding |
|---|---|---|---|
| B1 | blocker | RPC 3.4 | Conflict hit dispositions and G2 clearances are kept only in process memory. Production also builds **two** separate `Checks` instances, so a clearance never reaches matter opening. |
| B2 | blocker | By-law 9 / sole practitioner | The money effects (bill approval and void, trust withdrawal and transfer, refund, write-off) refuse a preparer who approves their own work, with no sole-practitioner exception. The gate layer and the DB `core.check_sod` both allow it. A sole lawyer cannot bill, pay out trust, or clear a conflict that they (or their agent) prepared. |
| B3 | blocker | Limitations | The limitation roll uses the **r.1.03 court calendar**, which includes the Saturday-substitute Mondays and Tuesdays. These days are not Legislation Act s.87 holidays, so periods ending on them come out **one day late**. |
| M1 | major | By-law 9 / sole practitioner | Gate kinds that need 2 approvers (trust signing authority, `core.trust_signatory`, opening-balance import, ID and file destruction, rule packs) can never reach quorum when the firm has one licensee. |
| M2 | major | By-law 9 signatories | Signing authority never re-checks licence status or account status. `Freeze` has no production caller, so a suspended or offboarded licensee keeps signing trust withdrawals. |
| M3 | major | By-law 7.1 ID | `conflict.CheckFunds` has no production caller, so client ID verification is never enforced on trust movements. The EFT, court-order and settlement exemptions are also missing. |
| M4 | major | AI safeguards | Provenance accepts numbers found inside **quoted untrusted text** (emails, documents) as the source for trust-withdrawal and payment amounts. This is a prompt-injection path into money proposals. |
| M5 | major | HST | Zero-rating for non-residents is the default for any foreign address, because `RecipientResident` defaults to false. Bad-debt relief has no arm's-length test (ETA s.231). |
| M6 | major | Write-off | The approver must be the responsible lawyer **and** must not be the preparer. When the responsible lawyer drafts the write-off, nobody can approve it. |
| M7 | major | By-law 9 records | Three required items are missing: the valuable-property record (s.18), tracking of the 25-day reconciliation deadline with an overdue alert, and the cash-refund condition for the fee exemption to the cash limit. |
| M8 | major | Deadlines | The holiday calendar covers only 2025–2028. Outside those years the s.88 roll and the business-day counts silently fall back to Sundays only. |
| M9 | major | Product control vs practice | A 24-hour cooling-off applies to every trust withdrawal and transfer, with no override. It blocks same-day real-estate closings and urgent payments. |
| M10 | major | RPC 3.3 / non-engagement | The F-INT-04 decline letter prompt invites the agent to state "any limitation period" (advice to a non-client). It also does not stop the letter from revealing a conflict or the other client. |
| m1–m9 | minor | various | See the minor findings section. |

---

## 1. LSO By-law 9 (trust accounting)

**Correct.**

- **Trust journal design.**
  - The trust journal is append-only, with signed amounts and corrections by reversal only (`migrations/0001_init.sql:2202-2242`, immutability triggers at 2364-2369).
  - Balances are derived, never stored (2296-2308).
  - Each client ledger is locked and checked for a negative balance at commit (2311-2362).
- **Withdrawals.**
  - No cash withdrawals and no payee of "cash" or "bearer" (2180, 2230; `trust/withdraw/request.go:98-106`).
  - Fee withdrawals need `bill_id` (2181).
  - The fee transfer is capped at the bill's unreserved balance under the ledger lock (3693-3697).
- **Fee transfers only on delivered bills.**
  - `trust/withdraw/request.go:71-83` requires the bill to be on the same matter, `DeliveredAt <= on`, and amount ≤ balance due.
  - The check re-runs at authorization (`trust/withdraw/effect.go:218-235`).
  - This is stricter than s.9(1)(c), which also allows "other written notification". Acceptable.
- **Overdraft.**
  - The ledger can never go negative (DM-052).
  - NSF reversals replenish exactly the shortfall from firm funds, under G6 approval, in the same transaction, and open a `trust.exception` (`trust/nsf.go:81-160`).
  - HTTP reports the signed balance plus `overdrawn` (decision HTTP-10). Correct.
- **Cash.**
  - Aggregate per matter ≥ $7,500, counting trust and operating cash together, under a per-matter lock (`trust/cashlimit.go:63-110`; `0001_init.sql:3968-3977`). Correct.
- **EFT.**
  - A Form 9A record must match the withdrawal, its amount, the authorizer, and be signed before execution (`trust/withdraw/eft.go:34-63`).
  - The executor must differ from the authorizer, except for a sole practitioner, and that exception is audited (`effect.go:302-314`). Matches By-law 9 s.12 (verify the exact two-person wording).
- **Monthly three-way reconciliation.**
  - It compares bank (adjusted), trust journal and client listing (`trust/recon/threeway.go:27-51`). The GL is correctly left out.
  - A signed month is immutable and locks postings for that month (DM-054).
  - The package is hashed, and only a superseding row can reopen a month.
- **Sole-practitioner sign-off.** A sole licensee may sign their own reconciliation, with an audit record (`trust/recon/signoff_pg.go:148-184`).

**Wrong / missing.**

- **B2 (blocker): the sole-practitioner exception stops at the effects.**
  - The effects reject preparer = approver unconditionally:
    - `trust/withdraw/effect.go:208-209`
    - `trust/withdraw/transfer_effect.go:96-97`
    - `trust/withdraw/request.go:154`
    - `billing/bill/approve.go:48-49`
    - `billing/bill/approve_effect.go:87-88`
    - `billing/bill/void.go:67`
    - `billing/bill/void_effect.go:85`
    - `billing/payment/allocate.go:389`
    - `billing/payment/refund_effect.go:129`
  - Meanwhile `gates/http_decide.go:255-262` accepts a sole-practitioner declaration, and `core.check_sod` (`0001_init.sql:3625-3633`) allows it with an audit row. The gate is approved, then the effect fails.
  - PG-WRITES-FIX made "preparer = the acting user" for agent work. So a sole lawyer who uses the agent can **never** authorize a trust withdrawal or approve a bill.
  - **Fix:** add one `gates.SoD(ctx, tx, preparer, approver, what, id)` helper that calls `core.check_sod`, so the DB decides and writes the audit row. Every effect calls it instead of comparing ids. Agents remain refused.
- **M1 (major): 2-approver kinds deadlock for a sole practitioner.**
  - Affected kinds: `trust.signing_authority`, `core.trust_signatory`, `platform.import_balances`, `conflict.id_destruction`, `platform.file_destruction`, `cal.rule_pack`, `platform.rule_pack`, `core.offboarding`, `work.external_sync` (`spec/gates.json`, `app/legal/gates/kinds.json`).
  - `gates/selfapproval.go:41` refuses self-approval of any irreversible kind outright.
  - A sole lawyer therefore cannot appoint themselves as a trust signatory, so no withdrawal can ever be authorized.
  - **Fix:** add `sole_owner_delay` (for example 24–72 h), as GA9 and GA11 already have. For irreversible kinds, allow sole approval after the delay plus a written declaration, recorded with `self_approval_exception`.
- **M2 (major): stale signing authority.**
  - `trust/signatory/service_pg.go:46-51,65-70` trusts the `is_licensee` value stored when the grant was made. It never joins `core.app_user` to check `licence_status='good_standing'`, `status='active'` or `offboarded_at IS NULL`.
  - `Freeze` (`signatory/contract.go:105-107`) has no production caller.
  - A bonded non-licensee's authority ignores the bond amount compared with the withdrawal amount.
  - **Fix:** in `canSignSQL`, join app_user and require a live licence (for a licensee) or `bond_cents >= amount` (for a non-licensee), and record the "exceptional circumstances" reason. Call `Freeze` from the `core.offboarding` and licence-status-change effects.
- **M7 (major): missing By-law 9 records and controls.**
  1. There is no **valuable-property record** (s.18; open question at `spec/legal/trust.json:2735`). It is mandatory whenever the firm holds property other than money in trust.
  2. There is no tracking of the **25-day reconciliation deadline**: no due date, overdue alert or readiness item (`spec/legal/trust.json:1289,1380` say month end + 25 days). This is a frequent LSO spot-audit finding.
  3. The **cash-limit fee exemption** (`trust/cashlimit.go:26-33`, `fees_refunded_in_cash`) is accepted without recording the condition that any refund be made in cash.
     - Trust can never pay out cash (2230), so the refund has to be made from operating. Nothing records that obligation.
     - The list has no "pursuant to court order" exception code (s.5, verify).
  - **Fix:**
    - (1) An additive `trust.valuable_property` table and screen.
    - (2) `recon.DueOn(month) = last day + 25` (verify), a daily readiness check and an alert to the designated lawyer.
    - (3) A matter flag `cash_refund_in_cash` set by the G6c effect and shown on refund screens; add a `court_order` code.
- **m1 (minor): reconciling items are not sign-checked.**
  - `trust/recon/threeway.go:32-42` lets an outstanding cheque carry a positive amount. `BankError` is a catch-all with free text.
  - **Fix:** enforce the sign per kind (deposit and in-transit positive; cheque negative). Add an `unexplained_difference` kind that opens a `trust.exception` automatically, so a month with a real shortfall can be explained and signed, but never silently.
- **m2 (minor): firm-payee rule enforced only at the agent tool.**
  - The rule "the firm's account may be the payee only for fees on a delivered bill" lives only in `agent/tools_write.go:256-290`. On the human path, the payee is free text (`withdrawal_request.payee`), and nothing stops a "disbursement" withdrawal paid to the firm.
  - The rule is also too strict as law: By-law 9 s.9(1)(b) lets trust reimburse the firm for disbursements already paid on the client's behalf, without a bill.
  - **Fix:** add purpose `reimburse_disbursement`, which needs a recorded paid expense id. Enforce the firm-payee rule in `trust/withdraw` for both the human and agent paths.
- **m3 (minor): reversed cash receipts still count.** `billing.cash_received_cents` counts reversed trust cash receipts. This is the conservative direction, but document it.

## 2. Rules of Professional Conduct (3.4, 3.3, 3.6, non-engagement)

**Correct.**

- **Walls.** Walls are Postgres-backed in production (`boot/events.go:76`, `boot/production.go:306`). Agent search filters out walled matters (`conflict/walls.go:86`). The agent never decides G2.
- **Clearing a run.** Clearance requires every hit to be disposed of as not a conflict, consent or wall, with a note (`conflict/check.go:322-352`).
- **Matter opening.** It refuses to open a matter without a G2 clearance (`matter/http_write.go:395`, `matter/lifecycle.go`).
- **Fees (3.6) and Solicitors Act.** Contingency, referral and fee-split rules are encoded (BIL-L-013..033 in `billing/contingency`, `feeshare`). Interest runs from one month after delivery (`billing/interest/months.go:26`). The rate must be shown and agreed, with the CJA default behind a flag that is off.

**Wrong / missing.**

- **B1 (blocker): conflict records are not persisted.**
  - Dispositions and G2 decisions live in `Checks.decisions` and `Checks.dispositions`, which are in-memory slices (`conflict/check.go:176-186`, 195-198, 285, 316, 350).
  - The schema already has the right tables, and nothing writes them:
    - `conflict.hit.disposition*` (`0001_init.sql:1035-1039`)
    - `conflict.clearance` (1043)
    - `conflict.consent` (1058)
  - Production builds Clearance on `conflict.NewChecks(...)`, which uses in-memory `memRuns` (`boot/clearance.go:49`). Matter `Lifecycle` builds a **different** `NewChecksWith(PGRuns)` (`boot/production.go:305`).
  - Effect: a G2 clearance is invisible to matter opening, and it is lost on restart or across replicas. Rule 3.4 needs a conflict record you can actually rely on.
  - **Fix:**
    - Add `PGDispositions` and `PGDecisions` writing `conflict.hit` and `conflict.clearance` in the decision transaction.
    - Build one `Checks` instance in boot and share it between Clearance, the HTTP routes and Lifecycle.
    - Remove `memRuns` from the production path.
- **Major: consent is just a note.**
  - `DispositionNeedsConsent` and `DispositionNeedsWall` accept a free-text note (`check.go:140-143`, 311-313).
  - Rule 3.4-2 consent must be informed consent, in writing or confirmed in writing (r.1.1 "consent").
  - **Fix:** require a `conflict.consent` row with `status='signed'` and a `document_id` before G2 can clear a hit disposed as `needs_consent` or `needs_wall`.
- **B2 also applies to conflicts.**
  - `conflict/check.go:366-372` (`approverErr`) and `gates/decision.go:29-30` refuse the preparer.
  - A sole practitioner can never clear their own conflict check, so they can never open a matter.
  - **Fix:** apply the same SoD helper, with an audited sole exception.
- **M10 (major): non-engagement letters.**
  - `intake/prompts/f-int-04.md:7` tells the agent to state "any limitation period the prospect should watch". Telling a non-client about a specific limitation period is advice that creates reliance.
  - `f-int-15.md:15-17` does it correctly: limitation risk is mentioned, no date is given.
  - Neither prompt forbids stating that a conflict was the reason for declining, or naming the other client. Doing so breaches the existing client's confidentiality (Rule 3.3).
  - **Fix:** align F-INT-04 with F-INT-15. The letter must give no limitation date. It must say the firm "is unable to act" without giving the reason, and must name no person other than the addressee.
- **m4 (minor):** `docs.engagement_letter` (G5) does not check that a joint-retainer matter carries the Rule 3.4-5 joint-retainer disclosure (verify).

## 3. Limitation periods and deadlines

**Correct.**

- **Limitations Act periods** (`limitation/statute.go`):
  - 2-year basic period, ending on the anniversary date (s.4, and s.5(2) presumption).
  - 15-year ultimate period (s.15), with minors, incapacity and concealment suspensions (s.15(4)).
  - Agreements before discovery are ignored (s.22(4)/(5)). This is the conservative reading.
  - No period for sexual-assault claims (s.16(1)(h)) or for undiscovered environmental claims (s.17).
  - Schedule periods replace the basic and ultimate periods (s.19).
  - Feb 29 rolls to Feb 28 (Legislation Act s.89).
  - A Saturday last day is not rolled forward (correct under s.87/s.88; fail-safe).
- **Rules of Civil Procedure:** r.3.01 counting (calendar days for 7 days or more, business days for under 7, roll forward), r.18.01/18.02, r.25.04, r.61.04 and r.62.02 periods (`rulepacks/ontario/civil-procedure.yaml`).
- **Sep 30.** Excluding Sep 30 is correct: Ontario court offices are open, and it is not in s.87 or r.1.03.
- **shift_date.** It refuses business-day shifts while the holiday pack is unverified (decision TOOL-DATES).

**Wrong / missing.**

- **B3 (blocker): the wrong calendar is used for the limitation roll.**
  - `boot/services_legal.go:21-25` attaches `ontario.Holidays()`, which is the **r.1.03 court calendar** (`rulepacks/ontario/holidays.yaml:1-8`).
  - That calendar includes Saturday/Sunday-substitute days (`holidays.yaml:47,61,62,65,71,77`: 2026-12-28, 2027-12-27/28, 2028-01-03, 2028-07-03, 2028-11-13). As I understand s.87 (verify), Saturday is not a holiday, and the Sunday-only substitutes do not create these Mondays.
  - Example: a limitation ending on Monday 2028-07-03 (Canada Day falls on a Saturday) is pushed by `statute.go:312` to Tuesday 2028-07-04. That is one day after the period actually expires, which is a malpractice exposure.
  - The comment at `services_legal.go:19` also says "holiday **or a weekend**", which contradicts `statute.go:303-307`.
  - **Fix:**
    - Add a separate `legislation_act_holidays` list for s.87 (named days plus Sunday substitutes only), and use it for `Statute`.
    - Belt and braces: compute with both calendars and return the **earlier** date as "last safe day".
    - Mark it (verify).
- **M8 (major): the calendar covers only 2025–2028** (`holidays.yaml:13`).
  - A 2-year period starting in 2027, any 15-year ultimate period, and any business-day count past 2028 silently lose holidays.
  - Losing a holiday shortens forward periods, which is safe, but makes backward periods (counted back from a hearing) late.
  - **Fix:** generate holidays by rule (Easter computus plus the fixed and nth-weekday rules) for any year. If a date falls outside the verified years, the calculation fails closed with `ErrCalendarRange`.
- **m5 (minor): missing rules.**
  - Not yet expressible: the Dec 21–Jan 6 service rule and periods counted back from a hearing (`civil-procedure.yaml:18-19`). Both are already queued.
  - Deemed-service dates (r.16.06 mail = 5th day; r.16.05 email after 4 p.m.) are also missing. Without them, trigger dates entered as "date sent" are wrong.
  - The Limitations Act s.11 suspension (assisted ADR) is missing. Omitting it gives an earlier date, so it is safe, but record why.

## 4. HST: place of supply and bad-debt relief

**Correct.**

- **Place-of-supply order** (`billing/tax/pos.go:33-53`): non-resident, then litigation, then real property, then address. Litigation in a federal court falls back to the address rule (verify).
- **ETA Sch. VI Part V s.23** exclusions are in `pos_nonresident.go:16-50`: litigation for individuals only after commencement, and real property in Canada.
- **Rates and invoices.**
  - Rates are stored as data.
  - The BN gets a Luhn check, default ON (decision BIL-T-061/85, agreed).
  - Agent disbursements are outside the tax base.
  - Interest is untaxed and shown on its own line.
- **Bad-debt relief.** The tax fraction is computed on (write-off − interest) (`billing/credit/writeoff.go:59-66`). Recovery re-includes the tax fraction (`credit.go:196-207`). Both correct under s.231 and s.231(3).

**Wrong / missing.**

- **M5a (major): non-resident is the default.**
  - `pos.go:58-60`: `nonResident = !RecipientResident && foreign address`. `RecipientResident` is a bool that defaults to false.
  - Any client with a foreign mailing address is zero-rated by default, including a Canadian resident living abroad temporarily or a Canadian company with a US billing office.
  - Zero-rating needs evidence of non-residence. If CRA reassesses, the firm pays the 13%.
  - **Fix:** make residency three-state (`resident`, `non_resident_attested`, `unknown`). `unknown` with a foreign address fails closed and asks the lawyer. Store who attested and when.
- **M5b (major): no arm's-length test.**
  - Relief under s.231 is available only if the recipient deals at arm's length (BIL-T-080 says so). `ApproveWriteOff` has no such input (`writeoff.go:42-75`).
  - **Fix:** add `ArmsLength bool` to the write-off draft, required. When it is false, `TaxCents = 0` and the reason is recorded. Also record the 4-year claim deadline on the HST report.
- **M6 (major): write-off approval deadlock.**
  - `writeoff.go:31-34` requires approver ≠ preparer **and** approver = responsible lawyer. When the responsible lawyer drafts the write-off, which is common, it can never be approved.
  - **Fix:** if the preparer is the responsible lawyer, accept another lawyer who holds `billing.credit` (or the owner), or a sole practitioner through the SoD helper.

## 5. AI safeguards

**Correct.**

- **The agent never approves.** It is refused at:
  - the kernel (`kernel/gate/decide.go:112-116`)
  - the app gate check (`gates/decision.go:27-28`)
  - every effect
  - self-approval (`gates/selfapproval.go:27-28`)
- **Preparer ≠ approver.** The preparer set includes an agent's acting-for user.
- **Gate payloads.** They are hashed (JCS), and a vote counts only for the exact payload version (`kernel/gate/quorum.go:28-43`).
- **Provenance fails closed.** If it is not wired, nothing is written (`agent/tools_write.go:170`).
- **Citations.** They have the kernel shape, and every quote must be cited (`kernel/agenttools/cite.go:31-59`).
- **Injection quoting.** Untrusted text travels only as an escaped JSON `text` field inside `{"untrusted":true}` (`kernel/agenttools/quote.go:13-37`).

**Wrong / missing.**

- **M4 (major): untrusted quotes can supply money amounts.**
  - `kernel/agenttools/provenance.go:183-189` records every number inside quoted untrusted text. `tools_write.go:192` (trust withdrawal) and `tools_payment_allocation.go:76` check against that same set.
  - Example: a client email that says "please send 4,500,000" makes 4500000 a "sourced" amount ($45,000 in cents), and units can be confused.
  - That is acceptable for minutes taken from a transcript (decision TIME-FIX). It is not acceptable for trust or payment amounts.
  - **Fix:** tag each recorded value with its origin (`structured` or `quoted`). Add `SourcedWriteStrict` / `CheckStrict`, which accepts only structured values. Use it for the trust withdrawal, trust transfer, payment allocation and write-down tools.
  - Better still: per PAYLOAD-BUILDERS, the server builds the amount from the bill or ledger record.
- **m6 (minor): provenance matches values, not fields.** Any number read in a run can fill any field. Show on the gate where each figure came from: tool, record, field.
- **m7 (minor): no check on legal authorities in drafts.**
  - Free-text drafts are exempt from provenance (decision 165, agreed), and no tool checks legal authorities.
  - When the research memo or letter features land, any case or statute citation pattern in a draft should be flagged "authority not verified" on the G8 or G-RES gate. Hallucinated case law in Ontario filings has already drawn judicial criticism (verify the specific case before citing it in product text).

## 6. Record retention, privacy and ID verification

**Correct.**

- Trust and financial records (RC-FIN) and the audit trail are kept 10 years and never purged in v1 (`docs/kinds.go:26-27`). This covers By-law 9 s.23.
- ID verification methods follow By-law 7.1 (`0001_init.sql:450`). The retention date is "later of end of retainer / last transaction + 6 years" (436).
- `id_destruction` destroys the artefacts and keeps the identity record (decision GE2-legal, agreed).
- Data is hosted in ca-central-1 (`deploy/helm/counsel/values.yaml:11`).

**Wrong / missing.**

- **M3 (major): ID verification is never enforced.**
  - `conflict/idverify.go:63` (`CheckFunds`) has **no production caller**. Trust receipts and payouts never check verification.
  - The exemption list (`idverify.go:30-35`) lacks:
    - electronic funds transfer (By-law 7.1 s.22, verify)
    - funds paid under a court order or to settle legal proceedings
  - `FundsReceipt` without verification only warns. By-law 7.1 requires verification when the lawyer engages in or gives instructions about the funds (verify timing).
  - **Fix:** call `CheckFunds` in `trust/receipt.go` (Receive) and in withdrawal authorization (`withdraw/service_pg.go`). A third-party payout blocks. A receipt opens a readiness task with a due date. Add the missing exemptions.
- **m8 (minor): RC-ID start date.** RC-ID MinYears 6 does not say what the clock runs from. Make the guard compute `retention_until = max(end of client relationship, completion of work) + 6 years`. The GE3-legal effect is not on main yet. No destruction while a matter is open or under a litigation hold.
- **m9 (minor): PIPEDA gaps.**
  - There is no breach-of-security-safeguards register (PIPEDA s.10.3: keep every breach for 24 months). It exists only in spec text.
  - RC-FIN "never purge" will eventually over-retain personal information (Principle 4.5.3). Plan a purge that runs 10 years after fiscal year-end, behind an approval.
  - The engagement letter and privacy notice should disclose that an AI model provider processes the data, and where.

---

## Review of AUTOPILOT.md legal decisions

| Decision | Verdict | Reason |
|---|---|---|
| BIL-T-061 / BN check default ON (l.29, 85) | Agree | Required by the ITC Information Regulations. The check is deterministic. |
| T-024b.2 assessment timing (l.35) | Agree | Solicitors Act ss.3–4: within 1 month as of right; to 12 months by order; after 12 months only in special circumstances. Payment needs special circumstances (s.11). |
| Sep 30 excluded (l.38) | Agree | Not in Legislation Act s.87 or r.1.03, and courts are open. |
| Q-031.1 months_after clamp / backward roll rejected (l.42, 67) | Agree | Matches s.89. Rejecting a backward roll is the safe choice until verified. |
| Cooling-off 24 h (l.54) | **Disagree as built** | It is a product control, not By-law 9. With no override it blocks same-day real-estate closings and urgent payments (M9). Keep it as the default, and add a per-gate "urgent" path: a second signatory, or a 1 h delay plus a reason. |
| Trust/ID checks fail closed (l.61) | Agree | |
| T-022.3 fees only after delivery, auto-apply on delivered (l.62) | Agree | By-law 9 s.9(1)(c). Also show the client the application on the next statement. |
| Interest from one month after delivery (l.63) | Agree | Solicitors Act s.33. |
| T-029.2 single gated Clearer (l.64) | Agree in principle | Production still builds two `Checks` instances (B1). |
| Trust book separate (l.65) | Agree | |
| Fees book = numbered bills view (l.84) | Agree | By-law 9 allows a chronological file of bills. |
| NSF opens exception, replenish, cash limit ≥ $7,500 (l.87) | Agree | Add the court-order exception and the cash-refund condition (M7). |
| CJA default rate (l.88) then flag off (l.96) | Agree with l.96, disagree with l.88 | BIL-L-010 says the amended s.33 sets no default rate (verify against the current text). Keeping it off is safe. |
| Limitations s.88 roll, "holiday or weekend" (l.97, 108) | **Partly disagree** | Saturday must not roll, and the code is right on that. Using the r.1.03 calendar is wrong (B3). |
| Withdrawal authority 72 h (l.120) | Agree | Product control, conservative. |
| Write-off pro-rata split (Q-PORT-15, l.122; Q-INTEG-12.1) | Agree | Consistent with ETA s.231 and the BIL-T-082 proportional-tax approach. |
| Q-PORT-24 sole self-sign (l.141) | Agree | Extend the same exception to every effect and 2-approver kind (B2, M1). |
| HTTP-10 reject flooring at 0 (l.161) | Agree | An overdrawn ledger must be visible. |
| FEAT-TOOL-21 provenance fail closed, payee = matter contact (l.173-176) | Agree | |
| FEAT-FIX-A firm payee only for fees on a delivered bill (l.194-198) | **Partly disagree** | Fine as a product restriction. As a statement of law it is too narrow: s.9(1)(b) allows reimbursing disbursements to the firm without a bill (m2). The human path also needs the rule. |
| HTTP-18 statement closing balance authoritative (l.200) | Agree | |
| PORT-16b relief excludes interest (l.252) | Agree | Add the arm's-length test (M5b). |
| Q-PG-BILL-FIX address province, export zero-rate (l.265-268) | Agree | Requires an attested non-residence status, not a default (M5a). |
| PG-WRITES three-way excludes GL (l.273) | Agree | |
| Q-FEAT-INT-11 non-engagement through G3 (l.330) | Agree | Fix the F-INT-04 prompt (M10). |
| Q-HTTP-16 receipt recorded at once, pending classification (l.217-218) | Agree | Keeps the books current. Classification must never delay the deposit itself. |
| Q-FEAT-DL-04 limitation second check (l.228) | Agree | This is the main malpractice risk. |
| Q-FEAT-CON-01 G2 preparer refusal (l.229) | Agree, except for sole practitioners (B2) | |
| F-CON-03 memo on file (l.239) | Agree | But the dispositions behind it are not persisted (B1). |
| JOB-ACTOR designated lawyer, fail closed (l.287, 312) | Agree | |
| CON-TWO id_destruction 2 approvers (l.288) | Agree for multi-lawyer firms | Deadlocks for a sole practitioner (M1). |
| Draft documents not provenance-checked (l.304) | Agree with caveat | Flag legal citations (m7). Client-facing figures still go through the lawyer gate. |
| Trust drafts through SourcedDraft (l.368) | Agree | |
| TIME-FIX quoted numbers allowed for time entries (l.348-351) | Agree for time | Not for trust or payment amounts (M4). |
| TOOL-DATES / SHIFT-FLAG refuse business-day shifts while unverified (l.353, 364) | Agree | |
| BIL-03 fee transfers pay the firm; ErrFeesNeedFirmPayee (l.375, 391) | Agree | |
| PLT-12 no close with money in trust (l.397) | Agree as product control | Provide the unclaimed-trust-funds path (Law Society Act s.59.6, verify) so stale balances can be resolved. |
| PAYLOAD-BUILDERS server-side payloads (l.404-408) | Strongly agree | This is the real fix for M4 and m6. |
| Sign-off package SHA-256 (l.410) | Agree | |
| GE2-legal id_destruction artefacts only, cal.rule_pack as proposals (l.412-414) | Agree | Base the retention clock on the completion of work (m8). |
| Q-PORT-15 write-off draft table (l.122) | Agree | Fix the approver deadlock (M6). |
| T-017.5 no negative reductions (l.59) | Agree | |
| Q-T-023.2bb writeoff second approval owner-only (l.83) | Agree | A sole owner triggers M1/B2; route it through the sole exception. |

---

## Fix packages (disjoint files)

**P1: Sole-practitioner separation of duties (B2, M1, M9)**

- `app/legal/gates/sod.go` (new)
- `app/legal/gates/selfapproval.go`
- `app/legal/gates/decision.go`
- `app/legal/gates/coolingoff.go`
- `app/legal/gates/kinds.json`
- `spec/gates.json`
- `app/legal/trust/withdraw/effect.go`
- `app/legal/trust/withdraw/transfer_effect.go`
- `app/legal/trust/withdraw/request.go`
- `app/legal/billing/bill/approve.go`
- `app/legal/billing/bill/approve_effect.go`
- `app/legal/billing/bill/void.go`
- `app/legal/billing/bill/void_effect.go`
- `app/legal/billing/payment/allocate.go`
- `app/legal/billing/payment/refund_effect.go`

The helper calls `core.check_sod`, so the DB decides and audits. Add `sole_owner_delay` to the 2-approver kinds, and allow irreversible kinds after the delay plus a declaration. Add an "urgent" cooling-off override (a second signatory, or a short delay plus a reason). Tests: a sole firm can approve a bill and a withdrawal with an audit row; a two-lawyer firm still refuses; an agent is always refused.

**P2: Conflict record persistence and consent (B1, conflict part of B2, consent finding)**

- `app/legal/conflict/check.go`
- `app/legal/conflict/clearance.go`
- `app/legal/conflict/runs_pg.go`
- `app/legal/conflict/decisions_pg.go` (new)
- `app/legal/boot/clearance.go`
- `app/legal/boot/production.go`

Write `conflict.hit` dispositions, `conflict.clearance` and `conflict.consent` in the decision transaction. Build one shared `Checks`. `needs_consent` / `needs_wall` require a signed consent document. `approverErr` uses the P1 SoD helper through an interface; this package does not touch the P1 files.

**P3: Limitation and holiday calendars (B3, M8, m5)**

- `app/legal/rulepacks/ontario/holidays.go`
- `app/legal/rulepacks/ontario/holidays.yaml`
- `app/legal/rulepacks/ontario/legislation_act_holidays.yaml` (new)
- `app/legal/rulepacks/ontario/civil-procedure.yaml`
- `app/legal/limitation/statute.go`
- `app/legal/boot/services_legal.go`

Keep a separate s.87 calendar for limitations, and take the earlier of the two dates. Generate holidays by rule for any year. Fail closed outside the verified range with `ErrCalendarRange`. Add r.16.06 deemed service. Golden tests for 2028-07-03 and 2028-01-03.

**P4: Trust records, signatories, ID verification (M2, M3, M7, m1, m2, m3)**

- `app/legal/trust/signatory/service_pg.go`
- `app/legal/trust/signatory/validity.go`
- `app/legal/trust/recon/threeway.go`
- `app/legal/trust/recon/due.go` (new)
- `app/legal/trust/cashlimit.go`
- `app/legal/trust/receipt.go`
- `app/legal/trust/withdraw/service_pg.go`
- `app/legal/trust/property.go` (new)
- a new app-block migration (`trust.valuable_property`)
- `app/legal/conflict/idverify.go`
- `app/legal/boot/effects.go` (Freeze from offboarding)

Live licence and bond checks. 25-day due date and overdue alert. Sign check per item kind plus an `unexplained_difference` item that opens an exception. Court-order cash code and the cash-refund flag. `CheckFunds` wired into receipts and payouts, with the EFT, court-order and settlement exemptions. Firm-payee rule and a `reimburse_disbursement` purpose on the human path.

**P5: Tax and AI money safeguards (M4, M5, M6, M10, m6, m7)**

- `kernel/agenttools/provenance.go`
- `app/legal/agent/tools_write.go`
- `app/legal/agent/tools_payment_allocation.go`
- `app/legal/agent/tools_draft_bill.go`
- `app/legal/billing/tax/pos.go`
- `app/legal/billing/tax/contract.go`
- `app/legal/billing/credit/writeoff.go`
- `app/legal/billing/credit/contract.go`
- `app/legal/intake/prompts/f-int-04.md`

Provenance values tagged as structured or quoted, with strict mode on trust and payment amounts. Three-state attested residency. Arm's-length input for bad-debt relief. Fix the write-off approver rule. In the F-INT-04 prompt: no limitation date, no conflict reason, no third-party names.
