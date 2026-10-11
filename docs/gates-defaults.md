# Gate defaults (for owner review)

Source: `spec/gates.json`, generated from `spec/eng/a2-interfaces.json` `gate_kinds` and the
owner decision of 2026-10-11 (task T-002L.2). Approver is the a2 capability the approver must
hold; the engine still refuses the preparer and the agent.

How each value was chosen:

- **risk**: the a2 value, raised to the owner floor when the floor is higher. Floor: high for
  anything moving money (trust, billing, payments, write-offs, credits), deadlines and limitation,
  conflicts and walls, access, roles and signatories, release outside the firm, data export or
  destruction; medium for internal records changes; low for internal drafts and filing. Never lowered.
  `irreversible` stays where a2 set it (the schema then requires 2 approvers).
- **min_approvers**: the a2 value, raised to 2 where the spec text names two people. Else 1.
- **ttl**: a2 `ttl_hours` (the spec states a TTL for every gate, so the owner's 24h/72h/7d
  defaults did not apply).
- **payload_schema**: a minimal object with the fields the spec screens or the a2 agent tool show.
  Extra properties are allowed so later tasks can add fields without a contract change.
- **spec_id**: the spec gate id as a2 writes it, kept in an additive optional schema field (Q-003).
- **dual_when** (Q-002, owner decision 2026-10-11): `signatories>=2` on the gates a2 marks
  dual mode (G-B4, G-B6, G6, G6c, G11, G11t, G12, G-TSIG, G-TREL). The gate then needs 2 distinct
  approvers whenever the firm has at least 2 eligible approvers holding the capability; a firm
  with one eligible approver keeps `min_approvers`.
- **sole_owner_delay** (Q-002): `24h` on GA9 bulk export and GA11 MFA reset, both set to
  2 approvers. When only one eligible approver exists, that person may approve alone after
  24h, and the firm is notified.

## Open points for the owner

1. G-IMP names three people (owner, lawyer, bookkeeper); a2 and this file set 2.
2. G-B5 and G9d add a managing lawyer above a threshold; the spec gives no threshold, so 1.
   No trust withdrawal threshold exists in the spec, so trust withdrawals stay at 1.

## Gates

| Slug | Spec id | Risk | Approvers | TTL | Reason |
|---|---|---|---|---|---|
| `billing.prebill_review` | G-B1 | high | 1 | 168h | billing: creates the bill draft that becomes money owed; risk raised from spec medium |
| `billing.bill_approval` | G-B2 | high | 1 | 168h | billing: numbers and delivers a bill, may apply trust |
| `billing.payment_allocation` | G-B3 | high | 1 | 72h | money: posts payments to bills or ledgers |
| `billing.trust_fee_withdrawal` | G-B4 | high | 1 | 72h | trust: moves money from trust to operating; dual when signatories>=2 |
| `billing.bill_void` | G-B2 | high | 1 | 168h | billing: voids a numbered bill |
| `billing.credit_note` | G-B5 | high | 1 | 168h | billing: credit or write-off reduces money owed |
| `billing.refund` | G-B6 | high | 1 | 72h | payments: returns money to the payer; dual when signatories>=2 |
| `billing.collections_message` | G-B7 | high | 1 | 72h | billing and release: firm-worded demand leaves the firm |
| `billing.settings_change` | G-B8 | high | 1 | 168h | billing: numbering, interest and approval rules |
| `billing.payment_plan` | G-B9 | high | 1 | 168h | payments: authorises scheduled charges |
| `trust.receipt_classification` | G6 | high | 1 | 72h | trust: posts a receipt to a client ledger; dual when signatories>=2 |
| `trust.cash_over_limit` | G6c | high | 1 | 72h | trust: accepts cash at or over the reporting limit; dual when signatories>=2 |
| `trust.withdrawal` | G11 | high | 1 | 72h | trust: money leaves trust; dual when signatories>=2 |
| `trust.transfer` | G11t | high | 1 | 72h | trust: moves money between client ledgers; dual when signatories>=2 |
| `trust.reconciliation_signoff` | G12 | high | 1 | 336h | trust: locks the trust month; dual when signatories>=2 |
| `trust.signing_authority` | G-TSIG | high | 2 | 72h | signatories: who can move trust money; approvers raised from spec 1: spec: owner with signing authority plus a second signatory (GA6 canonical, CF-07); dual when signatories>=2 |
| `trust.regulatory_release` | G-TREL | high | 1 | 72h | release: trust reports and audit packs leave the firm; dual when signatories>=2 |
| `gl.operating_reconciliation` | G12g | medium | 1 | 336h | internal records: closes the operating month; no money moves |
| `timekeeping.accept` | G9 | medium | 1 | 336h | internal records: agent suggestion becomes a real time entry; risk raised from spec low |
| `timekeeping.disbursement` | G9b | high | 1 | 168h | billing: expense becomes billable; risk raised from spec medium |
| `timekeeping.timesheet` | G9c | high | 1 | 168h | billing: staff time becomes billable; risk raised from spec medium |
| `timekeeping.writedown` | G9d | high | 1 | 168h | billing: write-down reduces money owed |
| `timekeeping.fee_arrangement` | G9e | high | 1 | 168h | billing: rates and fee terms the client pays |
| `docs.engagement_letter` | G5 | high | 1 | 168h | release: letter and fee agreement go to the client |
| `docs.outgoing` | G8 | high | 1 | 72h | release: document leaves the firm |
| `docs.filing_privilege` | G-D1 | medium | 1 | 168h | internal records: filing and privilege flag on firm documents |
| `docs.template_publish` | G-D2 | medium | 1 | 336h | internal records: new template version used in assembly |
| `docs.precedent` | G-D3 | medium | 1 | 336h | internal records: precedent becomes visible to agent and other matters; risk raised from spec low |
| `cal.confirm` | G7 | high | 1 | 24h | deadlines: date becomes authoritative |
| `cal.second_check` | G7b | high | 1 | 24h | deadlines: critical date verified |
| `cal.change` | G7c | high | 1 | 24h | deadlines: active date changes |
| `cal.mark_met` | G7d | high | 1 | 72h | deadlines: reminders stop; risk raised from spec medium |
| `cal.release` | G7r | high | 1 | 72h | deadlines: deadline leaves the active set |
| `cal.rule_pack` | G7e | irreversible | 2 | 336h | deadlines/limitation: date engine rules change |
| `cal.calendar_delegation` | G-CAL | high | 1 | 168h | access: another user edits a calendar or titles leave via feed; risk raised from spec medium |
| `conflict.clearance` | G2 | high | 1 | 168h | conflicts: matter may open |
| `conflict.consent_text` | G2a | high | 1 | 168h | conflicts and release: consent letter leaves the firm |
| `conflict.wall` | G2b | high | 1 | 72h | walls: activate or lift an ethical wall |
| `conflict.lateral` | G2c | high | 1 | 168h | conflicts/walls: joiner account activation |
| `conflict.client_id` | G4 | high | 1 | 168h | money and conflicts: unlocks trust receipts and payments for the client |
| `conflict.id_destruction` | G14 (conflicts) | irreversible | 2 | 168h | data destruction: ID records deleted |
| `intake.lead_decision` | G1 | high | 1 | 168h | release: decline or referral letter leaves the firm; limitation exposure; risk raised from spec medium |
| `intake.consult_outcome` | G3 | high | 1 | 168h | limitation: deadline enters the calendar; letters leave the firm; risk raised from spec medium |
| `intake.public_wording` | G-form | high | 1 | 336h | release: public disclaimer and consent wording; risk raised from spec medium |
| `intake.fee_waiver` | G-fee | high | 1 | 168h | payments: refund or waiver of a collected fee; risk raised from spec medium |
| `intake.open_matter` | G-open | high | 1 | 168h | conflicts and release: matter created, documents released, portal invite |
| `matter.party_conflict` | G2 (matters) | high | 1 | 168h | conflicts: new conflict-relevant party |
| `matter.opening` | G-MO | high | 1 | 168h | conflicts and money: unlocks trust requests and agent work |
| `matter.key_fact` | G-KF | high | 1 | 168h | deadlines: key facts drive deadline computation; risk raised from spec medium |
| `matter.stage_move` | G-ST | high | 1 | 168h | billing and release: stage triggers client-facing, billing or closing actions; risk raised from spec medium |
| `matter.contact_merge` | G-CM | high | 1 | 168h | conflicts: changes what the conflict index sees; risk raised from spec medium |
| `matter.agent_note` | G-NT | medium | 1 | 336h | internal records: note enters the matter record; risk raised from spec low |
| `matter.practice_config` | G-CF | medium | 1 | 336h | internal records: fields, stages and flags |
| `matter.bulk_change` | G-BK | high | 1 | 72h | access: bulk reassignment changes who sees matters |
| `matter.renumber` | G-NUM | irreversible | 2 | 72h | internal records at scale: numbers of opened matters |
| `matter.widen_access` | G-ACC | high | 1 | 72h | access: wider visibility on a restricted matter |
| `matter.apply_template` | G-AT | medium | 1 | 168h | internal records: fields, folders and work items on an open matter |
| `work.output` | G-W1 | medium | 1 | 168h | internal records: item done; outbound release has its own gate |
| `work.plan` | G-W2 | medium | 1 | 168h | internal records: agent may start template items |
| `work.workflow_publish` | G-W3 | medium | 1 | 336h | internal records: workflow or template version |
| `work.external_sync` | G-W4 | high | 2 | 168h | data export: matter data leaves to an external tracker; approvers raised from spec 1: spec: responsible lawyer + admin |
| `work.automation` | G-AR | high | 1 | 168h | agent autonomy: rule creates items or drafts without a person |
| `work.absence` | G-W5 | high | 1 | 168h | deadlines: re-routes deadline reminders; risk raised from spec low |
| `platform.portal_message` | G-PM | high | 1 | 72h | release: substance reaches the client |
| `platform.outgoing_email` | G-OUT | high | 1 | 72h | release: email leaves the firm |
| `platform.summary_accuracy` | G-SUM | medium | 1 | 336h | internal records: summary becomes the matter record; risk raised from spec low |
| `platform.closing` | G13 | high | 1 | 168h | trust and data: trust refund, closing letter, retention |
| `platform.file_destruction` | G14 (platform) | irreversible | 2 | 168h | data destruction: archived file contents |
| `platform.skill_autonomy` | G-SK | high | 1 | 168h | agent autonomy: agent sends without a per-message gate |
| `platform.access_change` | G-PERM | high | 1 | 72h | access/roles: users, roles, walls |
| `platform.integration_scope` | G-INT (platform) | high | 1 | 168h | data export: data flows to a connected system or outside Canada |
| `platform.export` | G-EXP | high | 1 | 72h | data export: reports and files leave the firm |
| `platform.guest_access` | G-GUEST | high | 1 | 72h | access: outside person sees matter content |
| `platform.research_memo` | G-RES | medium | 1 | 168h | internal records: memo filed as work product; outgoing use has its own gate |
| `core.role_change` | GA1 (access) | high | 1 | 72h | access/roles |
| `core.user_activation` | GA2 (access) | high | 1 | 72h | access: user with approve, trust or firm-wide access |
| `core.temp_grant` | GA3 | high | 1 | 24h | access: extra access beyond role |
| `core.matter_visibility` | GA4 | high | 1 | 72h | access: restrict or open a matter |
| `core.wall_lift` | GA5 | high | 1 | 72h | walls: removes a wall or screened person |
| `core.trust_signatory` | GA6 | high | 2 | 72h | signatories: who can move trust money; approvers raised from spec 1: spec: owner with signing authority plus a second signatory |
| `core.break_glass_review` | GA7 | high | 1 | 168h | access: closes an emergency access record |
| `core.review_close` | GA8 | high | 1 | 336h | access: closes the quarterly access review; risk raised from spec medium |
| `core.bulk_export` | GA9 | high | 2 | 72h | data export: firm-wide export; approvers raised from spec 1: spec: two owners; sole owner after 24h delay |
| `core.sign_in_settings` | GA10 | high | 1 | 72h | access: SSO, MFA, lockout, sessions |
| `core.mfa_reset` | GA11 | high | 2 | 24h | access: clears MFA enrolment; approvers raised from spec 1; sole owner after 24h delay |
| `core.offboarding` | GA12 | high | 2 | 168h | access: hands over matters and closes an account; approvers raised from spec 1: spec: responsible lawyer of each matter plus owner for final cut-off |
| `core.vendor_support` | GA13 | high | 1 | 24h | access: vendor staff into the firm cell |
| `core.authority_limit` | GA14 | high | 1 | 72h | access: per-person authority limit |
| `platform.security_answer` | G-SEC | high | 1 | 168h | release: answers go to an insurer or client; risk raised from spec medium |
| `platform.outbound_connector` | G-INT (readiness) | high | 1 | 168h | data export: data leaves the firm or Canada |
| `platform.payment_provider` | G-PAY | irreversible | 2 | 72h | payments: online payments start |
| `platform.import_balances` | G-IMP | irreversible | 2 | 168h | trust: opening balances |
| `platform.rule_pack` | G-RULE | irreversible | 2 | 336h | deadlines/limitation: province rule pack |
| `platform.vendor_support` | G-SUP | high | 1 | 24h | access: vendor staff into firm data |
| `platform.escrow_destination` | G-ESC | high | 1 | 168h | data export: firm data to external storage |
| `platform.service_credit` | G-SLA | high | 1 | 168h | credits: issues a service credit; risk raised from spec medium |
| `platform.published_price` | G-PRICE | high | 1 | 168h | billing: public price change; risk raised from spec medium |
