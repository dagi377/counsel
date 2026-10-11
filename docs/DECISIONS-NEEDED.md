# Decisions needed

> **Status 2026-10-11:** the owner adopted every recommendation below, so no blocker stops the build.
> - **CF-\* items:** decided.
> - **V-\* items:** working assumptions. Their code ships behind feature flags that are off by default, and they must be confirmed by a licensed Ontario lawyer (or an accountant, for tax) before go-live.
>
> `spec/blockers.json` carries `status` and `decision` for each item.

Counsel is still a spec. Some build tasks are waiting on a decision or a legal check. This page lists every blocker named in `spec/eng/o1-tasks.json` (field `blocked_by`), ordered by how many tasks each one holds up. The same data is in `spec/blockers.json`.

Who answers:

- **owner**: you, as the firm owner (a business choice).
- **lawyer/LSO**: a lawyer reads the official LSO by-law, rule or statute text and confirms or corrects the spec.
- **accountant/CRA**: your accountant checks the tax treatment against CRA sources.
- **engineering**: a technical decision. Nothing for you to do; listed so you can see why work is waiting.

"Flag" means the work can be built and tested now but stays switched off for any firm until the answer arrives. The build guide allows this for any rule marked (verify). Nothing here is legal advice. Where the spec says (verify), it is not confirmed.

## Answer these 5 first

1. **V-TRUST** (Trust accounting rules (LSO By-Law 9)): unblocks 6 trust tasks, including Clio cut-over. A lawyer reads LSO By-Law 9.
2. **CF-02** (Shape of the trust ledger): the only open owner decision on how trust money is recorded. Blocks 4 tasks.
3. **V-BILL** (Solicitors Act timing and bill wording): a lawyer confirms the Solicitors Act timing and the bill wording. Blocks 3 billing tasks.
4. **CF-07** (Who approves a change of trust signatory): a one-line owner confirmation that changing a trust signatory needs two people. Blocks 3 tasks.
5. **CF-15** (How long to keep financial, audit and ID records): an owner yes/no: keep all financial and ID records in v1 (no deletion).

Engineering's own top item is **CF-03** (missing tables, 6 tasks). It needs no input from you.

## Summary

| Id | Question | Recommendation | Answerer | Status | Tasks blocked |
|---|---|---|---|---|---|
| [CF-01](#cf-01) | Should every rate and percentage (tax, discount, markup, interest, contingency share) be stored in one unit, parts per million, with money in whole cents? | Parts per million everywhere | engineering | resolved in spec | 7: T-003, T-002L, T-014, T-017, T-018, T-024, T-025 |
| [V-TRUST](#v-trust) | Do our trust rules match the current text of LSO By-Law 9? We could not read it (lso.ca returned HTTP 403). | A lawyer confirms each listed By-Law 9 rule | lawyer/LSO | needs verification | 6: T-011, T-012, T-013, T-022, T-038L, T-041 |
| [CF-03](#cf-03) | Can the missing plumbing and billing tables (outbox, inbox, idempotency keys, gate versions, interest charges, client credit, trust fee transfers, write-offs, feature flags) be added to the first database migration? | Add them to the first migration now | engineering | fix in progress | 6: T-008, T-008b, T-007, T-002L, T-021, T-040b |
| [CF-02](#cf-02) | Should the trust records be kept as a separate per-client trust transaction book (the By-Law 9 book of record), with the general ledger holding only the operating (firm) account? | Separate trust book of record | owner, accountant/CRA | open | 4: T-010, T-002L, T-010L, T-011 |
| [CF-05](#cf-05) | Should the database schema names be the single source for module names (for example 'gl' not 'books', 'cal' not 'deadlines')? | Database schema name = code package = module | engineering | resolved in spec | 4: T-002, T-007, T-002L, T-010b |
| [CF-07](#cf-07) | When someone is added to or removed from the trust signing authority, should it need two people (an owner with signing authority plus a second signatory)? | Two-person approval (GA6) | owner | resolved in spec | 3: T-007L, T-011, T-039L |
| [CF-12](#cf-12) | How should coders handle 12 record types defined two different ways and 29 record types with no fields defined yet? | Use the data model's merges; file a question for each empty one | engineering, owner | open | 3: T-006L, T-028, T-029 |
| [V-BILL](#v-bill) | Are the Solicitors Act one-month periods counted as calendar months from delivery, and is our rules numbering and assessment-notice wording correct? | A lawyer confirms | lawyer/LSO | needs verification | 3: T-019, T-024, T-024b |
| [CF-10](#cf-10) | Is the tax point the earlier of payment and the bill's due date, with 'due' meaning the bill date, and a trust retainer not counting as payment until applied? | Yes, per BIL-T-050/051 | engineering, accountant/CRA | fix in progress | 2: T-019, T-024 |
| [CF-11](#cf-11) | Should paying the firm's fees out of trust use one approval type, needing one trust signatory, allowed only after the bill is delivered? | One approval kind with one signatory | owner | resolved in spec | 2: T-012, T-022 |
| [CF-15](#cf-15) | Should the system never delete financial, audit or client-ID records in version 1, until the legal retention periods are confirmed? | Keep everything; no purge in v1 | owner, lawyer/LSO | open | 2: T-011, T-032L |
| [CF-04](#cf-04) | Which names do approval records use? | Data-model tables with slug codes | engineering | resolved in spec | 2: T-007, T-007L |
| [CF-06](#cf-06) | With one database per firm, is it fine that messages carry a constant firm id but data tables do not? | Constant firm id in messages only | engineering | resolved in spec | 2: T-002, T-002L |
| [CF-08](#cf-08) | Can a time entry or expense appear again on a new bill after the old bill is voided? | Yes, uniqueness only among non-void bills | engineering | fix in progress | 2: T-016, T-020 |
| [CF-09](#cf-09) | Should each approved bill permanently record its interest rate, place of supply for tax, signer and signature, with the bill date equal to the approval date? | Add and freeze them at approval | engineering | fix in progress | 2: T-017, T-019 |
| [CF-13](#cf-13) | When a client pays more than they owe, should the excess sit briefly as unapplied, then become client credit that must be applied, refunded or moved to trust within a set number of business days? | Short-lived credit, 5 business days default | owner, lawyer/LSO | resolved in spec | 1: T-021 |
| [CF-16](#cf-16) | Should the AI need approval before auto-deleting documents, and send e-sign reminders or warm-transfer calls only from templates and lists you approved in advance? | Approval for deletion; pre-approved templates for reminders and transfers | owner | resolved in spec | 1: T-032L |
| [V-CFA](#v-cfa) | Are the contingency fee cap formula and the required LSO standard form (and its date) correct? | Lawyer confirms | lawyer/LSO | needs verification | 1: T-027 |
| [V-DEADLINES](#v-deadlines) | Has a lawyer checked every Ontario deadline rule in our rule pack against the Rules of Civil Procedure and the Limitations Act, 2002? | Lawyer checks each entry before switch-on | lawyer/LSO, owner | needs verification | 1: T-031L |
| [V-ID](#v-id) | Are the client ID verification exemptions and the 6-year record retention under By-Law 7.1 Part III correct? | Lawyer confirms | lawyer/LSO | needs verification | 1: T-029 |
| [V-SURCHARGE](#v-surcharge) | May the firm add a credit-card surcharge, up to what cap, where is it banned, and is it subject to HST? | Keep it off until confirmed | accountant/CRA, lawyer/LSO | needs verification | 1: T-026 |
| [V-TAX](#v-tax) | Are our rules right for (1) where a litigation service is supplied for HST purposes and (2) Indian Act s.87 relief for First Nations clients? | Accountant confirms both | accountant/CRA | needs verification | 1: T-014 |
| [CF-14](#cf-14) | If the place of supply changes after a bill is approved, should the bill be voided (before delivery) or credited (after), rather than sent back to draft? | Never return to draft | engineering | resolved in spec | 1: T-020 |
| [CF-20](#cf-20) | Should a bill be voidable only before it is delivered, approved by someone other than its preparer, with a credit note required after delivery? | Void only before delivery, by a second person | engineering | fix in progress | 1: T-020 |

24 blockers: 17 conflicts (CF) and 7 verifications (V). 15 need someone outside engineering.

## CF-01

**How percentages are stored.** Status: resolved in spec.

**Question.** Should every rate and percentage (tax, discount, markup, interest, contingency share) be stored in one unit, parts per million, with money in whole cents?

**Options.**

- Parts per million everywhere (13% = 130000)
- Basis points (13% = 1300), which cannot hold Quebec QST 9.975%

**Recommendation.** Parts per million everywhere: it is the only unit that holds QST 9.975% exactly, and the spec has already been restated this way.

**Who answers.** engineering

**Blocks.** T-003 (kernel/money: int64 cents, rhu, ppm); T-002L (Import legal rule, gate and access data); T-014 (Tax rates and place-of-supply engine); T-017 (Bill draft builder: lines, reductions, discounts); T-018 (Bill tax computation and totals); T-024 (Interest on overdue accounts); T-025 (Split bills, batch billing, combined client bills)

**Can work proceed behind an off flag?** No flag needed. The spec marks this resolved; the engineering lead only has to clear it from the task list.

**Notes.** Engineering only; nothing for the owner to decide.

**Sources.** o1-tasks.json conflicts CF-01; b2 rate_unit 'ppm (13% = 130000)'.

## V-TRUST

**Trust accounting rules (LSO By-Law 9).** Status: needs verification.

**Question.** Do our trust rules match the current text of LSO By-Law 9? We could not read it (lso.ca returned HTTP 403).

**Options.**

- A lawyer reads By-Law 9 and confirms or corrects each listed rule
- Ship the trust features without confirmation (not acceptable)

**Recommendation.** A lawyer confirms each listed By-Law 9 rule: trust money is the highest-risk area for an Ontario firm, and every trust rule in the spec is marked (verify).

**Who answers.** lawyer/LSO

**Blocks.** T-011 (Trust: accounts, signatories, client ledgers, receipts); T-012 (Trust withdrawals, transfers, EFT authorization); T-013 (Trust bank import and monthly three-way reconciliation); T-022 (Trust applied to fees); T-038L (Legal reports: AR aging, WIP, trust listing, HST report); T-041 (Clio importer: financial cut-over and validation)

**Can work proceed behind an off flag?** Yes. The build guide says these tasks may be coded and tested behind a flag that is off. They cannot be switched on for a firm until the text is confirmed.

**Notes.** Items to check, as listed in the spec (all marked verify): cash limit and exceptions, ss.4-6 (spec says receiving cash aggregating $7,500 or more on one client file is barred; RV1-004 flags the 'or more' wording); withdrawals for fees only after a bill is delivered, s.9; withdrawal methods, s.10; electronic transfer authorization and Form 9A, s.12; fees book and records, s.18; retention, ss.21-23 (spec says trust journals 10 years, other financial records 6 years, unverified); monthly reconciliation timing. Also: how many business days a client overpayment may sit in the operating account before it must be refunded, applied or moved to trust (spec default 5, verify; RV1-006, b3 BIL-C-067).

**Sources.** o1-tasks.json V-TRUST; b1 BIL-L-034 to BIL-L-042 (all verify); b1 sources_not_reachable.

## CF-03

**Missing database tables.** Status: fix in progress.

**Question.** Can the missing plumbing and billing tables (outbox, inbox, idempotency keys, gate versions, interest charges, client credit, trust fee transfers, write-offs, feature flags) be added to the first database migration?

**Options.**

- Add them to the first migration now (nothing is released yet)
- Add them later as separate migrations

**Recommendation.** Add them to the first migration now: nothing is released, so there is no upgrade cost; coders must not invent tables.

**Who answers.** engineering

**Blocks.** T-008 (kernel outbox, inbox, idempotency keys); T-008b (kernel inbound webhooks inbox); T-007 (kernel/gate: approval engine, decisions, effects); T-002L (Import legal rule, gate and access data); T-021 (Payments: capture, allocation, overpayment, refunds, reversals); T-040b (Clio alongside sync)

**Can work proceed behind an off flag?** No. These tasks build the tables themselves; they wait for the data-model addendum (status: resolving).

**Notes.** Engineering only. It blocks the most kernel work, so it is the top engineering item.

**Sources.** o1-tasks.json CF-03 (a2 ARC-I-008/015/023/030, ARC-U-019/022; b3 BIL-I-020, BIL-C-052/063/072/085).

## CF-02

**Shape of the trust ledger.** Status: open.

**Question.** Should the trust records be kept as a separate per-client trust transaction book (the By-Law 9 book of record), with the general ledger holding only the operating (firm) account?

**Options.**

- Separate trust book of record; trust totals derived from it; general ledger is operating only
- One double-entry journal for trust and operating, tagged by book

**Recommendation.** Separate trust book of record: it mirrors the client trust ledgers By-Law 9 requires and keeps trust money physically apart from firm money in the data.

**Who answers.** owner, accountant/CRA

**Blocks.** T-010 (kernel/ledger: double-entry engine, periods, reversals); T-002L (Import legal rule, gate and access data); T-010L (Operating books: chart of accounts and posting templates); T-011 (Trust: accounts, signatories, client ledgers, receipts)

**Can work proceed behind an off flag?** Partly. Operating-account work can start; trust ledger work waits because the data shape itself is in question. Status: open, needs owner confirmation.

**Notes.** This is a real owner decision. Your bookkeeper or accountant should agree that the trust listing and three-way reconciliation they expect can be produced from a per-client trust book (the spec says: bank control = sum of client ledgers, per trust account).

**Sources.** o1-tasks.json CF-02 (b3 BIL-I-001, BIL-I-002, BIL-I-009 vs a4 trust.trust_transaction).

## CF-05

**Module and table names.** Status: resolved in spec.

**Question.** Should the database schema names be the single source for module names (for example 'gl' not 'books', 'cal' not 'deadlines')?

**Options.**

- Database schema name = code package = module
- Keep the interface document's names and map them

**Recommendation.** Database schema name = code package = module: one name per thing; the interface spec has already been updated to match.

**Who answers.** engineering

**Blocks.** T-002 (Import frozen contracts and contract tooling); T-007 (kernel/gate: approval engine, decisions, effects); T-002L (Import legal rule, gate and access data); T-010b (Matter core: matter, matter_member, minimal party (no conflicts UI))

**Can work proceed behind an off flag?** No flag needed. Marked resolved in the spec.

**Notes.** Engineering only.

**Sources.** o1-tasks.json CF-05; a2 module_rename / renamed_gate_kinds.

## CF-07

**Who approves a change of trust signatory.** Status: resolved in spec.

**Question.** When someone is added to or removed from the trust signing authority, should it need two people (an owner with signing authority plus a second signatory)?

**Options.**

- Two-person approval (access gate GA6)
- One approval by the responsible lawyer (trust gate G-TSIG)

**Recommendation.** Two-person approval (GA6): it is the stricter control on who can move client money; the spec already adopted it.

**Who answers.** owner

**Blocks.** T-007L (Legal gate kinds and self-approval policy); T-011 (Trust: accounts, signatories, client ledgers, receipts); T-039L (Trust signatory change gate)

**Can work proceed behind an off flag?** No flag needed. Marked resolved (GA6 canonical, G-TSIG routes to it). Owner: confirm you agree.

**Notes.** Owner confirmation only.

**Sources.** o1-tasks.json CF-07; legal-spec trust.json and access.json.

## CF-12

**Incomplete entity definitions.** Status: open.

**Question.** How should coders handle 12 record types defined two different ways and 29 record types with no fields defined yet?

**Options.**

- Use the data model's merge decisions; for empty ones, stop and file a question
- Let coders choose fields

**Recommendation.** Use the data model's merges; file a question for each empty one: invented fields in a law-practice system become compliance and migration debt.

**Who answers.** engineering, owner

**Blocks.** T-006L (Legal roles, capabilities, walls resolver); T-028 (Parties, contacts, relationships (merged entity)); T-029 (Conflicts: checks, hits, clearance, walls, ID records)

**Can work proceed behind an off flag?** Partly. Work proceeds on the defined fields; each empty record type becomes a question. The owner is asked only when a field's business meaning is unclear. Status: open.

**Notes.** Mostly engineering. Expect a handful of short questions such as 'what does a Role record need to hold'.

**Sources.** o1-tasks.json CF-12; o2 lint baseline.

## V-BILL

**Solicitors Act timing and bill wording.** Status: needs verification.

**Question.** Are the Solicitors Act one-month periods counted as calendar months from delivery, and is our rules numbering and assessment-notice wording correct?

**Options.**

- A lawyer confirms the counting method, the RPC r.3.6 numbering and the assessment notice text
- Ship with the spec's reading unconfirmed

**Recommendation.** A lawyer confirms: these dates decide when the firm may sue on a bill and what the client is told about assessment.

**Who answers.** lawyer/LSO

**Blocks.** T-019 (Bill approval, numbering, immutability, signature, delivery); T-024 (Interest on overdue accounts); T-024b (Assessment, collection holds and fee repayment)

**Can work proceed behind an off flag?** Yes, off-by-default flag per the build guide.

**Notes.** The spec counts months as 'same day n months later, clamped to month end', with the delivery date taken in Toronto time (b1 conventions, verify with counsel). Interest: the spec says the Act sets no default rate and no maximum has been prescribed under s.33(5) (verify). The build guide lists T-019 and T-024; o1-tasks.json also lists T-024b.

**Sources.** o1-tasks.json V-BILL; Solicitors Act, R.S.O. 1990, c. S.15 ss.2, 33 (b1 BIL-L-003, BIL-L-004, BIL-L-007, BIL-L-010; verify); LSO RPC r.3.6 (b1 BIL-L-027 to BIL-L-033, verify).

## CF-10

**When HST becomes payable on a bill.** Status: fix in progress.

**Question.** Is the tax point the earlier of payment and the bill's due date, with 'due' meaning the bill date, and a trust retainer not counting as payment until applied?

**Options.**

- Yes, per b2 BIL-T-050 and BIL-T-051
- Some other reading

**Recommendation.** Yes, per BIL-T-050/051: this fixes a wrong cross-reference in the tax spec; the reading itself should be sanity-checked by the accountant.

**Who answers.** engineering, accountant/CRA

**Blocks.** T-019 (Bill approval, numbering, immutability, signature, delivery); T-024 (Interest on overdue accounts)

**Can work proceed behind an off flag?** No. Engineering fixes the reference (status: resolving); the accountant check can follow.

**Notes.** Accountant: please confirm 'due = bill date' matches how the firm reports HST (verify).

**Sources.** o1-tasks.json CF-10; b2 BIL-T-002, BIL-T-050, BIL-T-051.

## CF-11

**Approval to pay fees from trust.** Status: resolved in spec.

**Question.** Should paying the firm's fees out of trust use one approval type, needing one trust signatory, allowed only after the bill is delivered?

**Options.**

- One approval kind (billing.trust_fee_withdrawal) with one signatory
- Two approvals (bill gate plus generic trust withdrawal)

**Recommendation.** One approval kind with one signatory: one clear control; the amount is capped at the unpaid bill and the available trust balance.

**Who answers.** owner

**Blocks.** T-012 (Trust withdrawals, transfers, EFT authorization); T-022 (Trust applied to fees)

**Can work proceed behind an off flag?** No flag needed for the gate choice (resolved). The tasks it blocks also wait on V-TRUST, which is flaggable.

**Notes.** Owner: confirm one signatory is enough for fee transfers in your firm.

**Sources.** o1-tasks.json CF-11; b1 BIL-L-035 (By-Law 9 s.9, verify).

## CF-15

**How long to keep financial, audit and ID records.** Status: open.

**Question.** Should the system never delete financial, audit or client-ID records in version 1, until the legal retention periods are confirmed?

**Options.**

- Keep everything; no purge feature in v1
- Build purge now using the spec's unconfirmed periods (10 / 6 years)

**Recommendation.** Keep everything; no purge in v1: over-keeping is safe and reversible; deleting early is not. Periods are still (verify).

**Who answers.** owner, lawyer/LSO

**Blocks.** T-011 (Trust: accounts, signatories, client ledgers, receipts); T-032L (Legal document kinds, matter folders, retention classes)

**Can work proceed behind an off flag?** Yes. Trust and document work proceeds; any purge stays off (not built) until a lawyer confirms the periods. Status: open.

**Notes.** Owner: agree to 'no purge in v1'. Lawyer: confirm the periods later, before any purge feature.

**Sources.** o1-tasks.json CF-15; b1 BIL-L-041 (By-Law 9 ss.21-23, verify periods); By-Law 7.1 ID records 6 years (verify).

## CF-04

**Approval (gate) names.** Status: resolved in spec.

**Question.** Which names do approval records use?

**Options.**

- Data-model tables, gate code = slug such as billing.bill_approval, plus a spec id column
- Interface-document tables

**Recommendation.** Data-model tables with slug codes: already applied in the spec (RV2-001).

**Who answers.** engineering

**Blocks.** T-007 (kernel/gate: approval engine, decisions, effects); T-007L (Legal gate kinds and self-approval policy)

**Can work proceed behind an off flag?** No flag needed. Marked resolved.

**Notes.** Engineering only.

**Sources.** o1-tasks.json CF-04; r-review-2 RV2-001.

## CF-06

**One database per firm.** Status: resolved in spec.

**Question.** With one database per firm, is it fine that messages carry a constant firm id but data tables do not?

**Options.**

- Constant firm id in messages only
- Add a firm id column to every table

**Recommendation.** Constant firm id in messages only: each firm already has its own database.

**Who answers.** engineering

**Blocks.** T-002 (Import frozen contracts and contract tooling); T-002L (Import legal rule, gate and access data)

**Can work proceed behind an off flag?** No flag needed. Marked resolved.

**Notes.** Engineering only.

**Sources.** o1-tasks.json CF-06 (a2 ARC-I-008, ARC-I-031).

## CF-08

**Re-billing after a void.** Status: fix in progress.

**Question.** Can a time entry or expense appear again on a new bill after the old bill is voided?

**Options.**

- Yes: uniqueness applies only among non-void bills
- No: once billed, never re-billed

**Recommendation.** Yes, uniqueness only among non-void bills: otherwise void-and-rebill is impossible, which b3 requires.

**Who answers.** engineering

**Blocks.** T-016 (Time entries, timers, suggestions, approvals); T-020 (Void and place-of-supply change)

**Can work proceed behind an off flag?** No. Waits for the data-model fix (status: resolving).

**Notes.** Engineering only.

**Sources.** o1-tasks.json CF-08 (b3 BIL-I-007, BIL-C-084).

## CF-09

**Fields a bill must freeze.** Status: fix in progress.

**Question.** Should each approved bill permanently record its interest rate, place of supply for tax, signer and signature, with the bill date equal to the approval date?

**Options.**

- Add and freeze these fields at approval
- Look them up live when needed

**Recommendation.** Add and freeze them at approval: the interest rate printed on the bill (Solicitors Act s.33(3)) and the signature (s.2(1)) must not change later.

**Who answers.** engineering

**Blocks.** T-017 (Bill draft builder: lines, reductions, discounts); T-019 (Bill approval, numbering, immutability, signature, delivery)

**Can work proceed behind an off flag?** No. Waits for the data-model addendum (status: resolving).

**Notes.** Engineering only.

**Sources.** o1-tasks.json CF-09; r-review-1 RV1-008, RV1-013; b1 BIL-L-001.

## CF-13

**Client overpayments.** Status: resolved in spec.

**Question.** When a client pays more than they owe, should the excess sit briefly as unapplied, then become client credit that must be applied, refunded or moved to trust within a set number of business days?

**Options.**

- Short-lived credit, default 5 business days, then a trust exception
- Hold credit in the operating account indefinitely

**Recommendation.** Short-lived credit, 5 business days default: the excess is client money, not earned fees; the 5-day default is (verify) against By-Law 9.

**Who answers.** owner, lawyer/LSO

**Blocks.** T-021 (Payments: capture, allocation, overpayment, refunds, reversals)

**Can work proceed behind an off flag?** No flag needed for the design (resolved). Owner: pick the number of days. Lawyer: confirm under V-TRUST.

**Notes.** The day count is a firm setting.

**Sources.** o1-tasks.json CF-13; b3 BIL-C-063, BIL-C-067; r-review-1 RV1-006.

## CF-16

**AI actions that reach outside the firm.** Status: resolved in spec.

**Question.** Should the AI need approval before auto-deleting documents, and send e-sign reminders or warm-transfer calls only from templates and lists you approved in advance?

**Options.**

- Auto-purge behind the deletion approval (G14); reminders and transfers only from pre-approved templates (G-SK)
- Let the AI do these without approval

**Recommendation.** Approval for deletion; pre-approved templates for reminders and transfers: nothing leaves the firm or is destroyed without a human having approved it.

**Who answers.** owner

**Blocks.** T-032L (Legal document kinds, matter folders, retention classes)

**Can work proceed behind an off flag?** No flag needed. Marked resolved; owner: confirm.

**Notes.** Owner confirmation only.

**Sources.** o1-tasks.json CF-16; legal-spec documents.json, platform.json, intake.json.

## V-CFA

**Contingency fee agreements.** Status: needs verification.

**Question.** Are the contingency fee cap formula and the required LSO standard form (and its date) correct?

**Options.**

- Lawyer confirms O. Reg. 563/20 cap formula and the standard-form date
- Ship unconfirmed

**Recommendation.** Lawyer confirms: an over-cap contingency fee is unenforceable and a conduct issue.

**Who answers.** lawyer/LSO

**Blocks.** T-027 (Fee agreements: contingency, fee splitting, referral fees)

**Can work proceed behind an off flag?** Yes, off-by-default flag.

**Notes.** Only matters if the firm takes contingency matters. Also covers fee splitting and referral fee caps in b1 BIL-L-030/031 (RPC r.3.6-5, 3.6-6, verify).

**Sources.** o1-tasks.json V-CFA; b3 BIL-C-033 (O. Reg. 563/20 s.1, per b1 BIL-L-020); b1 BIL-L-017 (O. Reg. 563/20 s.7(1), 7(3): LSO Standard Form Contingency Fee Agreement dated November 18, 2021, verify).

## V-DEADLINES

**Court and limitation deadline rules.** Status: needs verification.

**Question.** Has a lawyer checked every Ontario deadline rule in our rule pack against the Rules of Civil Procedure and the Limitations Act, 2002?

**Options.**

- Lawyer checks each (verify) entry before the pack is switched on
- Ship unconfirmed (not acceptable: missed deadlines are malpractice risk)

**Recommendation.** Lawyer checks each entry before switch-on: a wrong computed deadline is a direct malpractice exposure.

**Who answers.** lawyer/LSO, owner

**Blocks.** T-031L (Ontario court rule pack and limitation assessment)

**Can work proceed behind an off flag?** Yes. The deadline engine and pack can be built behind an off flag.

**Notes.** Owner decision also open in deadlines.json open_questions: does the vendor maintain central Ontario rule packs with outside-counsel sign-off, or does each firm verify its own? This affects liability.

**Sources.** o1-tasks.json V-DEADLINES (16 entries per the build guide); legal-spec deadlines.json (RCP R.R.O. 1990, Reg. 194 rr.1.03, 3.01, 3.02, 16, 18.01; Limitations Act, 2002, S.O. 2002, c. 24, Sched. B ss.4, 5, 6, 7, 16, 22).

## V-ID

**Client identification (LSO By-Law 7.1).** Status: needs verification.

**Question.** Are the client ID verification exemptions and the 6-year record retention under By-Law 7.1 Part III correct?

**Options.**

- Lawyer confirms By-Law 7.1 Part III exemptions and retention
- Ship unconfirmed

**Recommendation.** Lawyer confirms: ID verification gates opening matters and moving money.

**Who answers.** lawyer/LSO

**Blocks.** T-029 (Conflicts: checks, hits, clearance, walls, ID records)

**Can work proceed behind an off flag?** Yes, off-by-default flag for the ID-record rules.

**Notes.** lso.ca was not reachable when the spec was written (b1 sources_not_reachable).

**Sources.** o1-tasks.json V-ID; b1 BIL-L-043 (By-Law 7.1 Part III, verify sections and exemptions).

## V-SURCHARGE

**Credit-card surcharges.** Status: needs verification.

**Question.** May the firm add a credit-card surcharge, up to what cap, where is it banned, and is it subject to HST?

**Options.**

- Keep the surcharge feature off until confirmed
- Confirm the card-network cap, provincial bans, the LSO position and CRA tax treatment, then enable

**Recommendation.** Keep it off until confirmed: the 2.4% cap and conditions come from card-network rules reported by processors, not cited law, and the HST treatment is (verify).

**Who answers.** accountant/CRA, lawyer/LSO

**Blocks.** T-026 (Card surcharges)

**Can work proceed behind an off flag?** Yes. Ships with surcharge_enabled = false (b3 BIL-C-101).

**Notes.** Also needs input from your card processor on the network rules. This is optional revenue, so it is low priority.

**Sources.** o1-tasks.json V-SURCHARGE and CF-18; b3 BIL-C-100/101 (Quebec Consumer Protection Act CQLR c P-40.1, verify section; LSO position, verify); b2 BIL-T-044 (ETA s.165; CRA position, verify); r-review-1 RV1-007.

## V-TAX

**Two HST edge cases.** Status: needs verification.

**Question.** Are our rules right for (1) where a litigation service is supplied for HST purposes and (2) Indian Act s.87 relief for First Nations clients?

**Options.**

- Accountant confirms both rules against CRA sources
- Ship with both unconfirmed

**Recommendation.** Accountant confirms both: wrong tax on a bill is the firm's liability with CRA.

**Who answers.** accountant/CRA

**Blocks.** T-014 (Tax rates and place-of-supply engine)

**Can work proceed behind an off flag?** Yes. The tax engine can be built; these two rules stay behind an off flag until confirmed.

**Notes.** Note: the related non-resident litigation question (RV1-001) was already checked against ETA Sch. VI Part V s.23(a) by the tax-spec owner; no change.

**Sources.** o1-tasks.json V-TAX; b2 BIL-T-012 (SOR/2010-117 s.27); b2 BIL-T-017 (Indian Act s.87; CRA TIB B-039R, verify).

## CF-14

**Changing tax jurisdiction on an approved bill.** Status: resolved in spec.

**Question.** If the place of supply changes after a bill is approved, should the bill be voided (before delivery) or credited (after), rather than sent back to draft?

**Options.**

- Never return to draft: void before delivery, credit note after
- Return the bill to draft

**Recommendation.** Never return to draft: approved bills stay immutable; already applied in the spec.

**Who answers.** engineering

**Blocks.** T-020 (Void and place-of-supply change)

**Can work proceed behind an off flag?** No flag needed. Marked resolved.

**Notes.** Engineering only.

**Sources.** o1-tasks.json CF-14; b3 BIL-C-207.

## CF-20

**Voiding a bill.** Status: fix in progress.

**Question.** Should a bill be voidable only before it is delivered, approved by someone other than its preparer, with a credit note required after delivery?

**Options.**

- Void only before delivery, by a second person; credit note after
- Allow void any time with a credit note

**Recommendation.** Void only before delivery, by a second person: a delivered bill has started the Solicitors Act clocks, so it must be corrected visibly, not erased.

**Who answers.** engineering

**Blocks.** T-020 (Void and place-of-supply change)

**Can work proceed behind an off flag?** No. Waits for the remaining data-model change (status: resolving).

**Notes.** Engineering only; owner may note the policy.

**Sources.** o1-tasks.json CF-20; b3 BIL-C-084; r-review-1 RV1-009.

## Not blocking any task

These conflicts are in the register but no task lists them in `blocked_by`:

- **CF-17** (database row-level security on wall-relevant tables): engineering, fix in progress (RV2-014).
- **CF-18** (card surcharge cap and tax): covered by V-SURCHARGE above; the feature stays off.
- **CF-19** (status lists differ between billing engine and data model): engineering, fix in progress.
