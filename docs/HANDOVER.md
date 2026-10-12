# Handover

Open work on Counsel, written so that the next agent can pick up any one item without reading the
build history first. The items are in priority order (P0 first). Each item gives:

- **Why**: the reason it matters.
- **Entry point**: the brief or spec to read, the branch if one exists, and the files to start in.
- **First command**: what to run first.
- **Acceptance**: the tests or behaviour that close the item.
- **Depends on**: what has to land first.

The rules are the same as for every task: `AGENTS.md`, one worktree per item, no `cd`, `git -C`,
`sed -i` or `cp`, and the mock model only. The general task brief is `docs/briefs/task-agent.md`
and the merge brief is `docs/briefs/integrator.md`.

Common setup (replace `<ID>` and `<base>`; `<base>` is `origin/main` unless the item names a branch):

    G=<path to the counsel clone>
    git --git-dir=$G/.git fetch origin
    git --git-dir=$G/.git worktree add <worktrees>/<ID> -b task/<ID> <base>
    env -u GOROOT make -C <worktrees>/<ID> check GO=/opt/homebrew/bin/go GOFMT=/opt/homebrew/bin/gofmt

Integration tests (`//go:build integration`) need a Postgres DSN in `COUNSEL_TEST_DB`:

    COUNSEL_TEST_DB=postgres://... go -C <wt> test -tags integration ./app/legal/<pkg>/...

---

## State of main

**Head.** `8b715ded` (pass 20 merge), plus the handover commit that adds this file. That commit
also restores the `matters.close` capability on the matter-closing screen (Q-INTEG-20).

**What works end to end (Go, unit tests and wired boot):**

- **Sign-in.** OIDC authorization code with PKCE, an HMAC-signed session cookie, and step-up
  (`kernel/identity/login`, AUTH-WIRE). In a dev environment, a dev login
  (`POST /auth/dev-login`) signs in one seeded user.
- **Roles.** `spec/legal/access.json` is embedded and registered. Each request reads the
  member's role from `core.app_user.role_id` (BOOT-CONFIG). Custom roles (`core.role_grant`) are
  not loaded: a member on a custom role gets 403.
- **Agent features.** Every feature in `spec/ai/features.json` has a registered Def
  (`app/legal/agent/featureset`). The features run from screen actions, events and schedules,
  on the mock model or a live provider. Gate payloads are built server-side from records
  (PAYLOAD-BUILDERS, PROD-WIRE-2). Provenance fails closed.
- **Approvals and effects.** Gates open with the acting user as preparer. Decisions go through
  the kernel gate engine (`POST /gates/{id}/decisions`, which needs step-up). Every declared gate
  kind is registered (`TestProductionRegistersEveryDeclaredGateKind`). Most kinds have a real
  per-kind effect (the GE2/GE3 passes). The exceptions are listed under the GE3-ops-b item below.
- **Jobs.** `counsel-worker` runs River jobs. These cover feature runs, the offboarding cut-off,
  the release sender, the destruction run and the scheduled ticks (JOB-RUNNERS, ENQUEUER).
- **Trust.** The trust journal is append-only. Ledgers never go negative. Receipts, withdrawals
  with Form 9A, transfers (TRANSFER-STORE), the NSF path, the cash limit and the monthly
  three-way reconciliation with a sealed sign-off all work.
- **Billing.** Bills (draft, approve, void, deliver), place of supply and HST, payments with GL
  posting (GL-FIX), payment plans, refunds, write-offs with bad-debt relief, and interest.
- **Documents.** Upload to an S3 bucket with SigV4 signing and a KMS key (DOC-UPLOAD). Without a
  bucket configured, the document routes answer 503.

**How to run it (dev):**

    go -C <wt> run ./cmd/counsel-migrate -db "$COUNSEL_DATABASE_URL"
    go -C <wt> run ./cmd/counseld -db "$COUNSEL_DATABASE_URL" -env dev \
        -dev-login -dev-login-email you@firm.test -model-mode mock
    go -C <wt> run ./cmd/counsel-worker -db "$COUNSEL_DATABASE_URL" -env dev -model-mode mock
    npm --prefix <wt>/web install && npm --prefix <wt>/web run dev

- **Flags.**
  - `counseld` takes `-addr` (default `:8080`), `-zone` (default `America/Toronto`), `-flags`
    (feature flags, comma-separated), `-firm-account` and `-seal-key-file`.
  - It also takes `-oidc-issuer`, `-oidc-client-id`, `-oidc-redirect-url` and
    `-oidc-step-up-acr`, plus `-model`, `-model-base-url` and `-model-protocol`.
  - The document bucket uses `-docs-bucket-url`, `-docs-bucket-region` and `-docs-kms-key-id`.
- **Environment only.** Secrets come only from the environment: `COUNSEL_OIDC_CLIENT_SECRET`,
  `COUNSEL_SESSION_KEY` (at least 32 bytes outside dev), `COUNSEL_SEAL_KEY`, `COUNSEL_MODEL_KEY`
  and the `AWS_*` credentials.
- **Dev defaults.** Dev uses a random seal key for each process. `-dev-login` is refused unless
  `-env dev`.
- **The dev user.** No seed creates the dev-login user. Insert an active `core.app_user` with a
  `role_id` before you sign in (`migrations/smoke.sql` shows the columns).
- **Mock model.** `-model-mode mock` points the agent at `-model-base-url`, normally the mock
  server `test/fakes/llm`. `deploy/counsel.dev.yaml` says an empty URL uses the in-process fake.

**What does not work yet:**

- **Web.** The web app is the design system (`web/src/ui`), the shell, session probing and
  access admin. It has no legal screens (T-045L). It has no portal server routes, and
  `PortalShell` is not mounted.
- **Legal (P0).** These P0 items are not on main: foreign-address clients and write-offs fail
  closed only once LAW-P5 merges, the SoD guard is not wired into the effects (LAW-P1), and none
  of LAW-P4 has started.
- **Events.** 49 bound events have no caller (EVENTS-CALL).
- **Integration tests.** Five integration packages fail on main.
- **E2e.** `test/e2e/suites/` does not exist, so `test/e2e/run.sh` runs nothing.
- **Gate kinds.** Ten `core.*` kinds have no effect. Four effects queue work that no worker
  picks up.

---

## P0 legal

### 1. LAW-P5: merge the tax and AI money safeguards

- **Why.** This is review M4, M5 and M10. Amounts in quoted untrusted text (emails, documents)
  can currently become trust and payment amounts. Foreign-address clients are zero-rated by
  default. The owner accepted the fail-closed behaviour before launch (Q-INTEG-20).
- **Entry point.**
  - Branch `origin/task/LAW-P5` (`14574829`, based on `52e70ed7`).
  - Read the branch's `docs/QUESTIONS.md` "LAW-P5" note and `docs/briefs/review-law.md` §4–5 and
    P5.
  - The blocking work is in `app/legal/platform/ai_f_plt_06.go` and `ai_f_plt_21.go`, their
    prompts and skills, and `app/legal/agent/tools_trust_balance.go` (`get_trust_balance`
    already exists).
- **First command.** Create the worktree from `origin/task/LAW-P5`, then:

      git --git-dir=$G/.git --work-tree=<wt> merge origin/main
      go -C <wt> test ./app/legal/platform/ -run 'FPLT06|FPLT21'

- **What to do.**
  - Add `get_trust_balance` to the Defs and grants of F-PLT-06 and F-PLT-21.
  - Change their prompts and skills so that the refund amount comes from that tool, not from
    the statement text.
  - Re-express F-PLT-21's overdraw case.
  - Carry `Provenance.ObserveTrigger` over to the tagged provenance. Trigger values are recorded
    as quoted.
- **Acceptance.**
  - The ten refused F-PLT-06 and F-PLT-21 feature tests pass, with no assertion weakened.
  - `kernel/agenttools` strict-mode tests pass.
  - A foreign-address client with no attestation gets `tax.ErrResidencyUnknown`.
  - A write-off with no arm's-length answer is refused.
  - `make check` is green. The branch is merged to main, and the merge is noted in QUESTIONS.
- **Depends on.** Nothing.

### 2. Record the non-resident attestation and the arm's-length answer (API and UI)

- **Why.** Once LAW-P5 lands, every foreign-address client and every write-off fails closed,
  and only a recorded answer releases them. No route or screen records either answer today.
- **Entry point.**
  - The LAW-P5 note: residency is read from `party.contact.custom_fields.tax_residency`
    (`status`, `attested_by`, `attested_at`). The write-off field is
    `billing.write_off_draft.arms_length` (migration 5644).
  - `api/openapi.yaml`: add a field to `CreditNoteCreate` and a contact residency write.
    Both are additive and need a QUESTIONS entry.
  - Server files: `app/legal/party`, `app/legal/billing/credit/writeoff_pg.go` and
    `app/legal/boot/services_billing.go`. Web: `web/src/features/billing/` and the contact
    screen.
- **First command.**

      grep -n 'CreditNoteCreate' -A30 <wt>/api/openapi.yaml

- **Acceptance.**
  - A lawyer can record a contact as resident or non-resident with an attestation. The
    attestation stores who attested and when, and the event is audited.
  - A write-off request carries `arms_length`.
  - The route tests cover 403, a 412 on a stale version, and an idempotent replay.
  - A draft for an attested non-resident is zero-rated, and one with an unknown status is held.
- **Depends on.** Item 1.

### 3. LAW-P1 boot wiring: SoD guard on each money step and the sole-practice rule

- **Why.** This is review B2 and M1. `gates.SoDGuard` exists, but no effect gets one in
  production, so a sole practitioner still cannot approve their own bill or trust withdrawal.
  The self-approval policy for the two-approver kinds (`sole_owner_delay`) is also not built.
- **Entry point.**
  - QUESTIONS "LAW-P1".
  - `app/legal/gates/sod.go` (`SoDGuard`, `PGSoDEvidence`, `CheckSoD`).
  - `app/legal/boot/gates_pg.go` (`gatePortsPG`) and `app/legal/boot/production.go`.
  - The effects in `trust/withdraw/effect.go`, `transfer_effect.go`,
    `billing/bill/approve_effect.go`, `void_effect.go` and `billing/payment/refund_effect.go`.
  - Kernel self-approval: `kernel/gate/selfapproval.go`, using `Catalog.SelfApproval`.
- **First command.**

      grep -rn 'SoD' <wt>/app/legal/boot <wt>/app/legal/trust/withdraw <wt>/app/legal/billing

- **What to do.**
  - Set `SoD: &gates.SoDGuard{Evidence: gates.PGSoDEvidence{}, Audit: <kernel audit log>}` on
    every money effect.
  - Build the kernel self-approval policy on `Catalog.SelfApproval`.
  - Pass the same policy to `conflict/check.go` `approverErr` through `gates.SoDApprover`.
    This is the conflict part of B2.
- **Acceptance.** A boot-level test shows four things:
  - A sole-licensee firm can approve its own bill and trust withdrawal, and each approval writes
    an audit row.
  - A two-lawyer firm is still refused.
  - An agent is always refused.
  - A two-approver kind can be confirmed by a sole licensee only after `sole_owner_delay`.
- **Depends on.** Nothing.

### 4. Cooling-off: end the wait early (kernel change)

- **Why.** This is review M9. The 24-hour cooling-off on trust actions blocks same-day closings.
  The override policy exists (`gates.CheckCoolingOffOverride`, audited as
  `cooling_off_override`), but the kernel engine runs cooling-off effects itself, so nothing can
  end the wait early.
- **Entry point.**
  - QUESTIONS "LAW-P1" (the open question is whether this is a kernel task or an app route).
  - `kernel/gate/decide.go` (lines 283 and 312, `effectiveAt`) and `kernel/gate/contract.go`
    (`RunDue`).
  - `app/legal/gates/sod.go`.
- **First command.**

      grep -n 'CoolingOff\|effectiveAt\|RunDue' <wt>/kernel/gate/*.go

- **What to do.** Make an additive kernel hook, for example `Engine.EndCooling(ctx, gateID,
  by, reason)`. It runs the effect now when the app's override policy allows it: a second
  signatory, or a short delay plus a reason. Mount it as `POST /gates/{id}/end-cooling`, an
  additive openapi path.
- **Acceptance.**
  - Kernel tests:
    - An allowed override runs the effect once and writes an audit row.
    - A refused override leaves the gate in `approved_cooling`.
    - A second call does nothing.
    - An agent is refused.
  - An app test runs the policy end to end.
- **Depends on.** Item 3, which provides the shared SoD evidence.

### 5. LAW-P4: trust records, signatories and ID verification (never started)

- **Why.** This is review M2, M3, M7, m1, m2 and m3, all By-law 9 and By-law 7.1 duties. None of
  the work has started.
- **Entry point.** `docs/briefs/review-law.md` §1, §6 and the P4 fix package, which gives the
  file list. The parts:

| Part | What | Start in |
|---|---|---|
| M2 | Cut off signing authority on suspension or offboarding: join `core.app_user` (`licence_status='good_standing'`, `status='active'`, `offboarded_at IS NULL`) in `canSignSQL`, require `bond_cents >= amount` for a bonded non-licensee, and call `Freeze` from the offboarding and licence-change effects | `app/legal/trust/signatory/service_pg.go`, `validity.go`, `app/legal/boot/jobs_offboarding.go`, `app/legal/access/gate_effects.go` |
| M3 | Verify ID at matter opening and at trust receipt: call `conflict.CheckFunds` from `trust/receipt.go` (Receive) and from withdrawal authorization; add the EFT, court-order and settlement exemptions | `app/legal/conflict/idverify.go`, `app/legal/trust/receipt.go`, `app/legal/trust/withdraw/service_pg.go` |
| M7.1 | Valuable-property record (s.18): an additive `trust.valuable_property` table (new trust-block migration) and a screen | `app/legal/trust/property.go` (new) |
| M7.2 | 25-day reconciliation: `recon.DueDate` and `OverdueAlerts` exist; wire the due date into readiness and send an alert to the designated lawyer (shared with item 8) | `app/legal/trust/recon/due.go` |
| M7.3 | Cash-refund flag: the G6c effect sets a matter flag `cash_refund_in_cash` that the refund screens show; add a `court_order` code | `app/legal/trust/cashlimit.go` |
| m2 | The s.9(1)(b) disbursement firm payee: purpose `reimburse_disbursement` with a paid expense id; enforce the firm-payee rule in `trust/withdraw` on both the human and agent paths | `app/legal/trust/withdraw/request.go`, `app/legal/agent/tools_write.go` |
| m1 | Sign-check reconciling items for each kind; an `unexplained_difference` kind opens a `trust.exception` | `app/legal/trust/recon/threeway.go` |
| m3 | Document that reversed cash receipts still count toward the cash limit | `app/legal/billing` (cash received) |

- **First command.**

      go -C <wt> test ./app/legal/trust/... ./app/legal/conflict/

- **Acceptance.** Each part has table-driven tests, tagged `// rule:`:
  - A suspended or offboarded signatory is refused at authorization.
  - An unverified third-party payout is blocked.
  - An unverified receipt opens a readiness task.
  - A month past its due date unsigned raises one alert.
  - A firm-paid disbursement reimbursement is allowed without a bill, and any other firm payee
    without a delivered bill is refused on both paths.
  - Migrations are additive and pass `migration-lint`.
- **Depends on.** Item 3 for the shared SoD helper. Merge item 1 first, because
  `agent/tools_write.go` overlaps with it.

### 6. M6: write-off approver deadlock

- **Why.** `credit` requires the approver to be the responsible lawyer and not the preparer.
  When the responsible lawyer drafts the write-off, which is common, nobody can approve it.
  LAW-P5 left this open.
- **Entry point.** `docs/briefs/review-law.md` §4 M6. The rule is in
  `app/legal/billing/credit/writeoff.go` (`ErrNotResponsible`).
- **First command.**

      go -C <wt> test ./app/legal/billing/credit/ -run WriteOff -v

- **What to do.** When the preparer is the responsible lawyer, accept another lawyer who holds
  `billing.credit`, or the owner. A sole practitioner goes through the SoD helper.
- **Acceptance.**
  - The table covers four cases: the responsible lawyer prepares and another lawyer approves;
    the owner approves; a sole firm approves through SoD with an audit row; an agent is refused.
  - The second approval rule (`billing.writeoff_second_approve`) still holds.
- **Depends on.** Item 1 (the same files) and item 3.

### 7. LAW-P2 open questions: conflict clearance and consent tables

- **Why.** Dispositions and run decisions are now persisted (migration 5204), but the contract
  tables `conflict.clearance` and `conflict.consent` are not written. Two lawyer questions are
  also open.
- **Entry point.**
  - QUESTIONS "LAW-P2". `conflict.clearance.approval_id` is a NOT NULL foreign key to
    `gate.approval_request`, but G2 is decided through `gates.CheckDecision` with no approval
    row. `consent.clearance_id` is NOT NULL, yet a consent must exist before G2 clears.
  - Files: `app/legal/conflict/check.go`, `decisions_pg.go`, `app/legal/boot/clearance.go`.
- **First command.**

      grep -n 'conflict.clearance\|conflict.consent' -A12 <wt>/migrations/0001_init.sql | head -60

- **Decide, then build.**
  - Run G2 through the kernel gate engine so that a clearance row exists to link to.
  - Does a consent withdrawn after clearance make the clearance stale? A lawyer must answer.
  - Does `needs_wall` require an active wall record? A lawyer must answer.
- **Acceptance.**
  - A cleared run writes one `conflict.clearance` row (memo, outcome, conditions) in the
    decision transaction, with its consents linked.
  - Whatever the lawyer decides about stale consent and walls is enforced at matter opening,
    with tests.
- **Depends on.** The lawyer answers (item 9), for the two policy questions.

### 8. LAW-P3 leftovers: lawyer questions, r.16.06, the s.11 note and reconciliation alerts

- **Why.** The calendars are rule-generated, but several points still need the lawyer.
  Deemed-service dates are missing, so trigger dates entered as "date sent" come out wrong.
  The 25-day reconciliation alert is not wired.
- **Entry point.**
  - QUESTIONS "LAW-P3", which lists the verify points: Civic Holiday in s.87, Saturday and
    Boxing Day substitutes, Sep 30, proclaimed days, and the rolling-window departure.
  - Files: `app/legal/rulepacks/ontario/holidays.go` and `civil-procedure.yaml`,
    `app/legal/limitation/statute.go`, `app/legal/trust/recon/due.go`, and the readiness
    wiring in `app/legal/boot`.
- **First command.**

      go -C <wt> test ./app/legal/rulepacks/... ./app/legal/limitation/ ./app/legal/trust/recon/

- **What to do.**
  - Add r.16.06 deemed service by mail (the 5th day) and r.16.05 email after 4 p.m.
  - Record in code and in QUESTIONS why the Limitations Act s.11 suspension is left out (it
    gives the earlier, safe date).
  - Wire `recon.OverdueAlerts` into the readiness check and a daily notification job for the
    designated lawyer.
- **Acceptance.**
  - Golden tests for the deemed-service dates.
  - A readiness item and one notification for an overdue month, and none once it is signed.
  - The lawyer's answers are applied, or recorded as still open.
- **Depends on.** Item 9 for the verify answers. The wiring can go ahead before them.

### 9. Lawyer re-audit

- **Why.** Every `(verify)` point needs a licensed Ontario lawyer before its flag goes on. The
  last audit was of `4c45ac3b`, and the P1, P2, P3 and P5 changes have landed (or will land)
  since.
- **Entry point.**
  - `docs/briefs/review-law.md`, the previous audit, for its method and ranks.
  - `docs/briefs/lawyer-checklist.md`, the open points.
  - `docs/DECISIONS-NEEDED.md` (V-TRUST, V-TAX, V-DEADLINES, V-ID).
- **First command.**

      grep -rn '(verify)' <wt>/app/legal <wt>/spec/legal | wc -l

- **Acceptance.** A new audit file in `docs/briefs/` covers main after items 1–8. It uses the
  same blocker, major and minor ranks, gives each lawyer-checklist point a verdict, and leaves
  no blocker open.
- **Depends on.** Items 1–8.

---

## P1 platform

### 10. EVENTS-CALL and EVENTS-9: pick one payload shape, then wire the callers

- **Why.** 49 bound events have no caller, so their features never fire. EVENTS-8 (on main) and
  EVENTS-9 define `trust.statement_received` and `trust.ledger_below_floor` twice, with
  different bodies:
  - EVENTS-8: `ledger_id`, `matter_id`, `balance_cents`, `floor_cents`, on `trust_ledger`.
  - EVENTS-9: `matter_id`, `user_id`, on `matter`.
- **Entry point.**
  - Branches `origin/task/EVENTS-9` (`4a19dfbd`) and `origin/task/EVENTS-CALL` (`f4a80f58`,
    which contains EVENTS-9). Both are based on `4c45ac3b`.
  - Read the branch notes "Q-EVENTS-9" and "Q-EVENTS-CALL", which list the 49 events and the
    `awaitingWritePath` guard.
  - Files: `app/legal/<pkg>/event_publishers.go`, `spec/eng/a2-interfaces.json` and the boot
    test `TestARC_I_030_*`.
- **First command.** Create the worktree from `origin/task/EVENTS-CALL`, then:

      git --git-dir=$G/.git --work-tree=<wt> merge origin/main

- **What to do.**
  - Choose one body per event. Keep EVENTS-8's ledger fields and add the `user_id` the trigger
    needs. This is a contract decision, so write a QUESTIONS entry.
  - Keep one Go type and one a2 entry for each event.
  - Then call each publisher from its write path, inside its transaction, as each owning path
    lands.
- **Acceptance.**
  - Both ARC-I-030 guards pass.
  - Each event in `awaitingWritePath` either has a caller and a write-path test that asserts
    exactly one outbox event, or keeps its line with a reason.
  - The branch is merged to main.
- **Depends on.** Nothing for the shape decision. Some callers wait on their write paths.

### 11. FEAT-CORE-16 and STRICT-SYNC

- **Why.** The spec-sync test should fail on any spec feature with no Def. FEAT-CORE-16 is not
  merged because agenttest's Seed has no trust-transfer ports, so `prepare_trust_transfer`
  shows up as a stand-in.
- **Entry point.**
  - Branch `origin/task/FEAT-CORE-16` (`bc6273e5`).
  - Files: `app/legal/agent/feature/spec_test.go` and `spec_sync_test.go`, and
    `app/legal/agent/agenttest/seed.go`.
- **First command.** Create the worktree from `origin/task/FEAT-CORE-16`, then:

      git --git-dir=$G/.git --work-tree=<wt> merge origin/main
      go -C <wt> test ./app/legal/agent/feature/ -run Spec

- **What to do.**
  - Give the Seed the `TrustTransfers`, `TrustAvailable` and `MatterTrustAccounts` ports.
  - Delete `pendingDefs` (every id in it now has a Def on main).
  - Set `strictSync = true`.
  - Never add a `pendingStandIns` entry.
- **Acceptance.** The strict spec sync is green, and `make check` is green.
- **Depends on.** Nothing.

### 12. GE3-ops-b: gate kinds with no effect

- **Why.** Approving these kinds changes nothing: `core.role_change`, `core.user_activation`,
  `core.temp_grant` (with expiry), `core.break_glass_review`, `core.review_close`,
  `core.bulk_export`, `core.sign_in_settings`, `core.mfa_reset`, `core.vendor_support` and
  `core.authority_limit`.
- **Entry point.**
  - QUESTIONS "Q-GE3-OPS-B" and "Q-GE2-OPS".
  - `docs/briefs/gate-effects.md`, the per-kind effect rules.
  - The `noEffect` list in `app/legal/boot/effects_approval_test.go`, and
    `spec/gates.json` for each kind's effect text.
  - `core.role_change` and `core.user_activation` need a production AdminStore.
- **First command.**

      grep -n 'noEffect' -A6 <wt>/app/legal/boot/effects_approval_test.go

- **Acceptance.**
  - Each kind has an idempotent effect, keyed by gate id, that refuses an agent approver and a
    preparer who approves their own work, with a test.
  - `temp_grant` expires: a job or a read-time check stops the grant at `expires_at`.
  - The `noEffect` list is empty.
- **Depends on.** Nothing.

### 13. JOB-RUNNERS leftovers

- **Why.** Several effects queue work that no worker picks up.
  - No crypto-shredder exists, so destruction orders stay `scheduled` with `ErrNoShredder`.
  - The rule "only a holder grants trust signing authority" is not enforced.
- **Entry point.**
  - QUESTIONS "Q-JOB-RUNNERS".
  - `app/legal/boot/jobs_platform.go` (`Options.Platform.Shredder`) and
    `app/legal/platform/runners*.go`.
  - `app/legal/boot/effects_approval.go`, for outbound comm items.
- **First command.**

      grep -n 'Shredder\|ErrNoShredder' -r <wt>/app/legal/platform <wt>/app/legal/boot | head

- **What to do.**
  - A shredder that destroys document versions and their KMS data keys. The owner decides
    whether it also covers backups.
  - Workers for the `platform.export` export assembler, imported opening balances
    (`platform.import_job` `committed`, which trust posts), vendor support expiry (set
    `revoked_at`), and outbound `comms.comm_item` with `delivery_status='queued'`.
  - Enforce the signing-authority grant rule in the `platform.access_change` effect.
- **Acceptance.**
  - Each worker has a test: one row moves once, and a retry does nothing.
  - An access change that grants trust signing authority is refused unless the approver holds
    it.
- **Depends on.** An owner answer on the shredder owner and on backups.

### 14. `intake.open_matter` payload fields in spec/gates.json

- **Why.** The effect needs the new matter's `matter_id`, `number` and `short_name` (and
  optionally `practice_area_id` and `visibility`), but `spec/gates.json` does not list them, so
  the payload builder cannot fill them.
- **Entry point.**
  - QUESTIONS "Q-GE3-OPS-B" (owner decision).
  - `spec/gates.json` (slug `intake.open_matter`), `app/legal/gates/kinds.json`, the payload
    builder registry (`agent.SpecPayloadBuilders`) and `app/legal/intake/gate_effects_lead.go`.
- **First command.**

      grep -n '"intake.open_matter"' -B2 -A30 <wt>/spec/gates.json

- **Acceptance.**
  - The fields are added to the payload schema (an additive change, with a QUESTIONS entry).
  - The builder fills them from the records.
  - The every-kind payload test covers the kind.
  - An approved gate opens the matter through `matter.opening`.
- **Depends on.** Nothing.

### 15. The `fee_waiver` refund flow

- **Why.** The `intake.fee_waiver` effect marks a collected fee as refunded for exactly
  `amount_cents`, but no money moves. The refund has to go through `billing.refund`, and
  nothing links the two.
- **Entry point.** QUESTIONS "Q-GE3-OPS-B" and "Q-GE2-OPS", `app/legal/boot/effects_ops.go`,
  and `app/legal/billing/payment/refund_effect.go`.
- **First command.**

      grep -n 'fee_waiver' -A20 <wt>/app/legal/boot/effects_ops.go

- **Acceptance.**
  - A waiver of a collected fee opens one `billing.refund` gate for the same amount and payee,
    with the waiver's approver as preparer.
  - Approving it moves the money once.
  - A waiver of an uncollected fee opens no refund.
- **Depends on.** Item 3, because the refund effect takes the SoD guard.

### 16. The five integration packages that fail on main

- **Why.** These five integration packages fail on main:
  - `app/legal/billing/bill`
  - `app/legal/billing/payment`
  - `app/legal/conflict`, the searcher (`search_pg_integration_test.go`)
  - `app/legal/trust/signatory`
  - `app/legal/trust/withdraw`

  The causes are stale seeds, a missing `payload_jcs` column in the seed inserts, and tables the
  seeds do not create.
- **Entry point.** The `*_integration_test.go` files in each package. The gate payload columns
  are in `migrations/0001_init.sql` (`gate.approval_request.payload_jcs` and
  `payload_sha256`).
- **First command.**

      COUNSEL_TEST_DB=postgres://... go -C <wt> test -tags integration ./app/legal/billing/bill/ ./app/legal/billing/payment/ ./app/legal/conflict/ ./app/legal/trust/signatory/ ./app/legal/trust/withdraw/

- **Acceptance.**
  - All five pass against a fresh database with every migration applied.
  - The seeds go through the shared migration helper, not hand-made DDL.
  - No assertion is weakened.
- **Depends on.** Nothing.

### 17. GE4: the S3 delete runs outside the transaction

- **Why.** `conflict.id_destruction` deletes the stored ID file before the deciding
  transaction commits. If the transaction rolls back, the file is already gone while the record
  says it is not.
- **Entry point.** QUESTIONS "Q-GE4-LEGAL", `app/legal/conflict/gate_effects.go` (around line
  363) and `kernel/docs` (`ObjectDeleter`).
- **First command.**

      grep -n 'DeleteObject' -B10 -A10 <wt>/app/legal/conflict/gate_effects.go

- **What to do.** Record the purge intent in the transaction (`file_purged_at` pending), then
  delete after commit through an outbox job that retries until the object is gone.
- **Acceptance.**
  - A rolled-back decision deletes nothing.
  - A committed one deletes exactly once.
  - A failed delete retries and never marks the file as purged early.
- **Depends on.** Nothing.

### 18. PROD-WIRE-2: unknown subject kinds open no gate

- **Why.** `boot.PGSubjectRecords` fails closed with `ErrSubjectKind` for any subject kind it
  does not map, so a feature whose gate is on such a subject silently never opens its gate in
  production.
- **Entry point.** QUESTIONS "Q-PROD-WIRE-2" and `app/legal/boot/subject_records_pg.go`
  (`subjectRecordSQL`).
- **First command.**

      grep -n 'subjectRecordSQL' -A40 <wt>/app/legal/boot/subject_records_pg.go

- **Acceptance.** A boot test lists every subject kind that a registered feature's gate or
  screen uses, and fails if `subjectRecordSQL` has no entry for it. Each missing kind gets its
  query.
- **Depends on.** Item 19, because the firm subject is one of these kinds.

### 19. FIRM-SCREENS

- **Why.** 19 firm-level screens still select a matter because main had no firm subject when
  they were built. Examples are trust reports, the reconciliation, workflow and access
  screens. There is a rule: a screen asks for the least its features need, and a firm draft
  takes its capability from the firm subject, never from a placeholder matter.
- **Entry point.**
  - `app/legal/boot/agent.go`, the `actionScreens` lines marked "firm-level" and "no firm
    subject yet" (`FirmSubject` now exists).
  - QUESTIONS entries Q-FEAT-WRK-04 and Q-FEAT-TRU-* ("until boot.FirmSubject").
- **First command.**

      grep -n 'firm-level\|no firm subject\|until boot has a firm' <wt>/app/legal/boot/agent.go

- **Acceptance.**
  - Each firm-level screen maps to `FirmSubject`, with the capability its features need.
  - The feature tests that pinned the matter subject are updated.
  - No feature drafts on a placeholder matter.
- **Depends on.** Nothing.

### 20. RUN-RECORDS-ONE

- **Why.** `AgentWiring` has two run-record stores that do the same job: `Records`
  (`boot.RunRecords`, the record-first wrapper) and `RunRecords` (`agent.RunRecords`, the
  `agent_feature_run` worker).
- **Entry point.** `app/legal/boot/agent.go` (around lines 898–943) and
  `app/legal/boot/run_record_first.go`. See the QUESTIONS note after "Q-FEAT-TRU-06".
- **First command.**

      grep -n 'Records' <wt>/app/legal/boot/agent.go <wt>/app/legal/boot/run_record_first.go

- **Acceptance.** One field and one interface remain. Both runners record through it, and the
  existing record-first and worker tests pass unchanged.
- **Depends on.** Nothing.

### 21. HELPER-DEDUP

- **Why.** Package billing still has two AI test-helper files: `ai_bil_helpers_test.go`
  (`billing_test`) and `ai_bilb_helpers_test.go` (internal, for F-BIL-05 and F-BIL-06).
- **Entry point.** QUESTIONS (the EVENTS-7 / PROV-UNBILLED note) and `app/legal/billing/`.
- **First command.**

      grep -ln 'package billing$' <wt>/app/legal/billing/*_test.go

- **Acceptance.**
  - The F-BIL-05 and F-BIL-06 tests move to `billing_test`, leaving one helper file.
  - The assertion count is unchanged.
- **Depends on.** Nothing.

---

## P1 web

### 22. W3 screens (T-045L.1 to T-045L.13)

- **Why.** The product has no legal screens. Matters, conflicts, intake, time, billing, trust,
  documents, deadlines, work, reports, access and settings are all missing, and so is the
  coverage check.
- **Entry point.**
  - `docs/briefs/web.md` (the bar and the rules) and `docs/briefs/review-ui.md` (the findings).
  - `spec/tasks.json` T-045L.* and `docs/TASKS.md`.
  - Build on `web/src/ui`: its primitives, templates (`ListPage`, `DetailPage`,
    `SettingsPage`), theme and `README.md`. Also build on the W2 foundation (session, shell,
    toast, theme).
  - Each area goes in `web/src/features/<area>/`.
- **First command.**

      npm --prefix <wt>/web install && npm --prefix <wt>/web test

- **Acceptance.**
  - Every screen in `spec/legal/access-map.json` for the area exists, with the loading, empty,
    error, partial and forbidden states. Each passes the axe sweep.
  - Money is shown in integer cents through `Money`, and an overdraft is always visible.
  - Gates show the preparer, the approver and the pending state.
  - Dates are in America/Toronto.
  - T-045L.13 `web/src/app/screens.test.ts` passes.
- **Depends on.** Item 23 for the user in the top bar. Item 24 for the portal screens.

### 23. A "who am I" endpoint

- **Why.** The web app guesses whether a session exists from `GET /v1/roles` (401 means signed
  out). It cannot show who is signed in, the firm, or the step-up time.
- **Entry point.**
  - QUESTIONS "W2 web foundation".
  - `api/openapi.yaml`: add `GET /v1/session` (user id, display name, firm, step-up time), an
    additive change.
  - `kernel/identity/login` and `web/src/kernel/session/session.ts`.
- **First command.**

      grep -n '/v1/roles' -r <wt>/web/src/kernel/session

- **Acceptance.**
  - The route answers 200 with the fields when a session exists, and 401 when none does.
  - The openapi conformance test passes.
  - The web probe uses it, and the top bar shows the user.
- **Depends on.** Nothing.

### 24. Portal server routes, then mount PortalShell

- **Why.** `kernel/portal` has auth, sessions and pages but no HTTP mount, so the web app
  renders portal features outside the member shell and never mounts `PortalShell`.
- **Entry point.**
  - QUESTIONS "W2 web foundation".
  - `kernel/portal` (`auth.go`, `pages.go`, `session_pg.go`) and `kernel/server` (`MountRoot`).
  - `web/src/kernel/portal/PortalShell.tsx` and `web/src/features/portal/`.
  - The web expects `/api/portal/matters` and `/api/portal/matters/{id}`.
- **First command.**

      grep -n 'func ' <wt>/kernel/portal/pages.go <wt>/kernel/portal/auth.go | head -30

- **Acceptance.**
  - Guest sign-in and the session work.
  - `/api/portal/*` serves only the guest's own matters, and a foreign matter answers 404.
  - `PortalShell` is mounted at `/portal`.
  - The portal tests cover first visit, expired session, empty and error.
- **Depends on.** Nothing.

### 25. Move the plain elements onto web/src/ui

- **Why.** Some screens still use raw `<button>`, `<table>`, `<input>` and `<select>` instead of
  the design system, so they miss the tokens, dark mode and the accessibility work.
- **Entry point.** `web/src/app/App.tsx`, `web/src/kernel/shell/states.tsx` and
  `web/src/kernel/access/AccessAdmin.tsx`. The components to use are in `web/src/ui/index.ts`.
- **First command.**

      grep -rlE '<(button|table|input|select)[ >]' <wt>/web/src --include='*.tsx' | grep -v /ui/

- **Acceptance.**
  - The grep above returns only test files.
  - Add a lint rule or test that keeps it that way.
  - The behaviour tests are unchanged.
- **Depends on.** Nothing.

### 26. The Fable UI re-review, which must reach on par

- **Why.** The last review (of `4c45ac3b`) gave the verdict **NOT YET**. The design system and
  foundation have landed since, but the UI has not been reviewed again.
- **Entry point.** `docs/briefs/review-ui.md` gives the method: 1280 and 1600 wide, light and
  dark, Playwright with system Chrome, mock data only. The bar is in `docs/briefs/web.md`.
- **First command.**

      npm --prefix <wt>/web run build

- **Acceptance.**
  - A new review file in `docs/briefs/` gives the verdict ON PAR, with no blocker or major
    open.
  - Every finding from the earlier review is closed or carried over with a reason.
- **Depends on.** Items 22–25.

---

## P2

### 27. The e2e suite

- **Why.** `test/e2e/run.sh` discovers `test/e2e/suites/*.sh`, but no suite exists, so the e2e
  gate in `docs/TASKS.md` passes without testing anything. AI-009 and AI-206 to AI-217 are the
  planned suites, all on the mock model.
- **Entry point.**
  - `test/e2e/run.sh` and `lib.sh`, `test/fakes/llm` and `deploy/counsel.dev.yaml`
    (`mode: mock`).
  - `docs/TASKS.md` AI-009 and AI-206 to AI-217, and `spec/ai/tasks-ai.json` for each
    feature's acceptance line.
- **First command.**

      bash <wt>/test/e2e/run.sh

- **What to do.** Start with one smoke suite that brings up Postgres, migrations, `counseld`
  (dev login, mock model) and `counsel-worker`. It signs in, opens a matter, runs one agent
  feature to a pending gate, approves it as a different user, and checks the effect. Then add
  one suite per area.
- **Acceptance.**
  - `test/e2e/run.sh` runs at least the smoke suite green on the mock model, with no network
    beyond the local stack.
  - CI runs it.
- **Depends on.** Item 16 (green integration packages). It is easier after item 23.
