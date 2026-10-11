# Questions

Open questions from implementers. Add one entry per question and stop the task it blocks.
The owner answers in place; move answered entries to the bottom.

Template:

```

## Q-NNN <short subject>

- Task: T-0NN
- Sources: <spec files and ids that disagree or are silent>
- Reading A:
- Reading B:
- Proposed:
- Answer: (owner)
```

## Open

## Q-T-045.1 Generated API client cannot live in web/src/kernel

- Task: T-045.1
- Sources: spec/tasks.json T-045.1 files (`web/src/kernel/api/client.ts`); AGENTS.md "Where things go" (generated client in `web/src/kernel`); kernel/boundary_test.go `termRoots` includes `web/src/kernel`; api/openapi.yaml is the legal API (`matter_id`, `/trust/...`, `hst_cents`).
- Problem: a client generated from api/openapi.yaml carries matter, trust and HST names, so TestKernelHasNoLegalTerms fails; `tools/kernel_allow.txt` may only shrink.
- Reading A: generate into `web/src/app/api/client.ts` (app layer); `web/src/kernel` keeps only the domain-free runtime (fetch, problem+json, extensible-enum decode, enumLabel).
- Reading B: keep the path and add `web/src/kernel/api/client.ts` as an exemption in kernel/boundary_test.go (scanner change, not an allowlist line).
- Proposed: A. A working generator (tools/gen_client.mjs, python3+PyYAML for YAML, no new npm deps) and ARC-I-002 tests are drafted and pass vitest; they ship once the path is decided.
- Answer: owner (2026-10-11): A. Generate into `web/src/app/api` (the kernel web stays free of legal terms), with a Node YAML parser (`yaml` devDependency), not python3. T-045.1 is requeued.

## Q-004 POST /hooks/{provider} lacks problem responses ARC-I-051 asks for

- Task: T-009.1
- Sources: spec/rules/a2.json ARC-I-051 ("every operation ... mutating operations 400, 401, 403,
  404, 409, 412, 422, 428, 429"); api/openapi.yaml `receiveProviderEvent` documents no 403, 404,
  409 or 422.
- Reading A: ARC-I-051 covers the session-less webhook receiver too; the contract needs an
  additive change adding the four responses.
- Reading B: the receiver is not a client operation; ARC-I-051 excludes it.
- Proposed: B for now. `TestARC_I_051_OperationsDocumentProblemResponses` skips only
  `/hooks/{provider}`; if A, add the responses additively and drop the skip.
- Answer: (owner)

## Q-AI-005 New fake-LLM endpoints must register routes in main.go

- Task: AI-005 (same for AI-006 and AI-007)
- Sources: spec/tasks.json AI-005/006/007 `files` list only the new protocol files; routes are
  registered in `server.handler()` in `test/fakes/llm/main.go`, which none of them lists.
- Reading A: touching `main.go` is implied; each task adds its `mux.HandleFunc` lines (and
  AI-006 hooks the fault layer and journal into `route`).
- Reading B: the tasks' `files` must be extended to list `main.go` first.
- Proposed: A. AI-005 adds two route lines to `handler()` plus `chat_test.go`. The AI-006 and
  AI-007 branches add their lines next to them; merging gives a trivial conflict in `handler()`.
## Q-AI-006 Fault layer and journal need main.go and messages_test.go

- Task: AI-006
- Sources: spec/tasks.json AI-006 `files` lists only `test/fakes/llm/faults.go` and
  `journal.go`; the server struct, flags and routes live in `test/fakes/llm/main.go`, and
  `TestFaultFixtureRefused` in `messages_test.go` asserted the pre-AI-006 501 refusal.
- Reading A: those edits are implied by the task.
- Reading B: the task's `files` must list them first.
- Proposed: A. `main.go` gains `-dump-bodies`, `-speed`, three server fields, the
  `/_journal` and `/_reset` routes and wraps `/v1/messages` in `s.instrument(...)`;
  `TestFaultFixtureRefused` is replaced by `faults_test.go`. When AI-005 merges, wrap
  `/v1/chat/completions` and `/v1/responses` the same way (a `decodeFunc` each, with
  `writeOpenAIError`).
## Q-AI-007 /v1/embeddings route registered in main.go

- Task: AI-007
- Sources: spec/tasks.json AI-007 `files` lists only `test/fakes/llm/embeddings.go`; routes are
  registered in `server.handler()` in `test/fakes/llm/main.go` (see Q-AI-005).
- Proposed: one `mux.HandleFunc("POST /v1/embeddings", ...)` line in `main.go`; the
  configured dimension is an `-embed-dim` flag (default 1024) declared in `embeddings.go`, so
  the server struct is untouched. When AI-006 merges, wrap the route in `s.instrument` if
  embeddings calls should be journalled.
- Answer: (owner)

## Q-AI-003 Wire the AI fixture lint into make check

- Task: AI-003
- Sources: spec/ai/tasks-ai.json AI-003 accept ("missing one -> make check fails naming it"),
  spec/ai/mocks.md sec. 4 ("tools/ai_fixture_lint.py, part of make check"); AI-003 `files`
  does not list `Makefile` (owned by T-001.2).
- Reading A: AI-003 adds the Makefile target itself.
- Reading B: the Makefile owner adds it; AI-003 only ships the lint.
- Proposed: add to `Makefile` (`check` prerequisites and .PHONY):
  `ai-fixture-lint:` running `$(PYTHON) -I tools/ai_fixture_lint.py --self-test` then
  `$(PYTHON) -I tools/ai_fixture_lint.py`. Default mode requires fixtures for every feature
  whose dir exists or whose fixture task is ticked in docs/TASKS.md, so main stays green while
  wave 1 lands; switch to `--strict` (all 194 features) once wave 1 is merged. Not done in
  AI-003 because Makefile is not in its files.

## Q-001 Vite and Vitest for web/

- Task: T-001.4
- Sources: AGENTS.md "Allowed dependencies" (Front end: TypeScript, React only); T-001.4 accept.
- Reading A: the front end may use only TypeScript and React, with no build or test tool.
- Reading B: web/ needs a bundler and a unit-test runner; feature tasks (e.g. T-035L.3) name Vitest.
- Proposed: allow Vite (build, dev server, `import.meta.glob` feature discovery) and Vitest
  (unit tests) as dev tooling for web/ only, plus @vitejs/plugin-react and TypeScript types.
  T-001.4 also adds a `web-test` target to the Makefile (not in its file list) so `make check`
  runs `npm --prefix web test`; it installs web/node_modules when missing. web/package-lock.json
  is not committed by T-001.4 because it is not in the task's file list; versions are pinned exactly.
- Answer: (owner)

## Q-T-007.1 registry.GateKind has no SpecID field for gate.approval_request.spec_gate

- Task: T-007.1
- Sources: migrations/0001_init.sql (approval_request.spec_gate NOT NULL); Q-003 added
  `spec_id` to spec/kernel/gatekind.schema.json only, not to the Go `registry.GateKind`.
- Reading A: add an optional `SpecID string` to `registry.GateKind` (additive contract change).
- Reading B: keep the contract; the engine takes the label separately.
- Done for now: B. `gate.Kinds.Label(slug, specID)` records it, and `Open` refuses a kind
  with no label (ErrInvalidKind). Proposed: A, so T-007L.1 can pass it through the Registry.
## Q-031.1 Rule pack schema cannot express clear days or months_after

- Task: T-031.1
- Sources: T-031.1 accept (day-count mode clear/business/calendar, months_after, validated by
  spec/kernel/rulepack.schema.json); kernel/deadline/contract.go (`Clear`, `Rule.MonthsAfter`,
  `Roll` none/forward only); spec/kernel/rulepack.schema.json (`day_count` enum calendar/business,
  no `months_after`, `roll` also allows `backward`; rules have `additionalProperties: false`).
- Reading A: the schema is the contract; packs cannot use clear days or months_after.
- Reading B: the schema is missing them; T-031.2 golden dates and the Ontario packs (T-031L.2)
  need both.
- Proposed: additive schema change: add `"clear"` to `day_count.enum` and an optional
  `"months_after": {"type": "integer", "minimum": 0}` to rule properties. kernel/deadline/pack.go
  already maps both, so no code change follows. Until then the loader rejects `roll: backward`
  (the contract has no `Roll` for it) as ErrInvalidPack; either add `RollBackward` to the
  contract or drop `backward` from the schema.
- Answer: (owner)

## Answered

## Q-004 POST /hooks/{provider} lacks problem responses ARC-I-051 asks for

- Task: T-009.1
- Sources: spec/rules/a2.json ARC-I-051 ("every operation ... mutating operations 400, 401, 403,
  404, 409, 412, 422, 428, 429"); api/openapi.yaml `receiveProviderEvent` documents no 403, 404,
  409 or 422.
- Reading A: ARC-I-051 covers the session-less webhook receiver too; the contract needs an
  additive change adding the four responses.
- Reading B: the receiver is not a client operation; ARC-I-051 excludes it.
- Proposed: B for now. `TestARC_I_051_OperationsDocumentProblemResponses` skips only
  `/hooks/{provider}`; if A, add the responses additively and drop the skip.
- Answer: owner (2026-10-11): A. 403, 404, 409 and 422 problem responses added to `receiveProviderEvent` in api/openapi.yaml (additive); the ARC-I-051 skip is gone and the contract test accepts additive changes over the baseline.

## Q-004 trust.Accounts cannot fill NOT NULL columns of trust_account and signatory

- Task: T-011.1
- Sources: app/legal/trust/contract.go (`Account` has only `AccountLast4`; `AddSignatory` takes
  only a user id); migrations/0001_init.sql (`trust_account.account_no_ciphertext NOT NULL`,
  `signatory.approval_id NOT NULL`, `signatory.is_licensee NOT NULL`).
- Reading A: the contract methods store placeholders (empty ciphertext, no approval id).
- Reading B: the contract needs additive input: the full account number (sealed before
  storage) and the GA1 approval id.
- Proposed: B. T-011.1 ships additive `AccountStore.OpenAccountNumber` and
  `AddApprovedSignatory`; the contract `OpenAccount` and `AddSignatory` validate and return
  `ErrNeedsApproval` until the owner picks a shape (e.g. `Account.AccountNo` and an
  `approvalID` argument).
- Answer: owner (2026-10-11): B. `Account.AccountNo` added (additive, input only). `OpenAccountNumber` and `AddApprovedSignatory` are the supported API; `OpenAccount` and `AddSignatory` stay as deprecated stubs returning ErrNeedsApproval, so no signature changes.

## Q-005 app/legal register test forbids any registration

- Task: T-010L.1
- Sources: T-010L.1 accept ("Chart OP-*/TR-* registered with the ledger engine");
  app/legal/books/register.go (features `add` a registrar from init());
  app/legal/register_test.go `TestRegisterWithNoFeaturesRecordsNothing` (owned by T-001.5)
  fails once any feature registers anything.
- Reading A: wave-1 features wire themselves with `add` from init(); the scaffold test is
  replaced by one that checks the expected calls.
- Reading B: features stay unwired until a later task.
- Proposed: A. T-010L.1 ships `books.RegisterCharts` unwired; once the owner of
  app/legal/register_test.go relaxes it, add `func init() { add(RegisterCharts) }` to chart.go.
- Answer: owner (2026-10-11): registration is split into an "always" set (chart of accounts, core gate kinds and capabilities) and a feature-gated set. `books.RegisterCharts` is wired from init(); `TestRegisterWithNoFeaturesRecordsOnlyAlwaysSet` asserts only always-set calls are recorded.

## Q-T-024b.2 Special circumstances after one month or after 12 months

- Task: T-024b.2
- Sources: spec/rules BIL-L-005; Solicitors Act ss.3, 4; the T-024b.2 accept line asked for a note after one month.
- Answer: owner (2026-10-11): follow BIL-L-005, not the accept line. Within one month assessment is available as of right; then the court may refer the bill ("court referral available"); special circumstances are needed only after 12 months. Tagged `// rule: BIL-L-005 (verify)`, behind `billing.solicitors_act_verified` (off by default).

## Q-GATES-001 spec/gates.json spec_id values that fail the spec_gate CHECK

- Task: T-007.1, T-007L.1
- Sources: migrations/0001_init.sql approval_request.spec_gate CHECK `^G(-?[A-Z0-9]+)+[a-z]?$`; spec/gates.json had 10 ids such as "G14 (conflicts)" and "G-form".
- Answer: owner (2026-10-11): the 10 ids are normalised (e.g. "G14 (conflicts)" -> "G14", "G-form" -> "G-FORM") and the long text kept in a new optional `label` field (additive in spec/kernel/gatekind.schema.json). `gates.ParseKinds` enforces the CHECK pattern; the frozen 0001 CHECK is unchanged.

## Q-INBOX-001 Two inbox Claim implementations

- Task: T-008.1, T-008.3
- Sources: kernel/idem `PG.Claim` and kernel/outbox `PG.Claim` both inserted into platform.inbox.
- Answer: owner (2026-10-11): keep `outbox.PG.Claim`; the idem copy is removed and its ARC-U-022/ARC-I-030 tests moved to kernel/outbox/inbox_test.go.

## Q-GATE-002 Fractional numbers in gate payloads

- Task: T-007.1
- Sources: kernel/gate payload canonicalisation rejects non-integer numbers; no test covered it.
- Answer: owner (2026-10-11): keep the rejection; `TestARC_I_014_PayloadHashRejectsFractionalNumbers` covers fractions, exponents and out-of-range integers.

## Q-005 BIL-C-082 over-credit: which status and problem code

- Task: T-023.1
- Sources: spec/rules/b3.json BIL-C-082 test ("-> 409 credit_exceeds_bill"); api/openapi.yaml
  requestCreditNote documents 409 but names no code.
- Reading A: 409 with problem code `credit_exceeds_bill`, rule BIL-C-082, as the rule's test says.
- Reading B: 422 validation error.
- Answer: owner (2026-10-11): A. `credit.CheckBucket` wraps `ErrExceedsBalance` together with a
  409 `credit_exceeds_bill` problem (rule BIL-C-082), so `server.WriteError` writes it as is.

## Q-004 Audit chain hashes `at` in the session time zone

- Task: kernel/db (T-043), kernel/audit (T-005)
- Sources: migrations/0001_init.sql audit.chain trigger (hashes `jsonb_build_object(..., 'at', NEW.at, ...)::text`); kernel/audit verification rebuilds the same text.
- Problem: jsonb renders a timestamptz in the session's TimeZone, so a caller whose session zone is America/Toronto makes the trigger hash a different string than verification rebuilds, and Verify reports a broken chain.
- Answer: owner (2026-10-11): kernel/db sets `SET LOCAL TIME ZONE 'UTC'` in every transaction (InTx and Bind), so the trigger and Verify hash `at` identically. Business dates stay America/Toronto through `clock`. Done in kernel/db/pool.go; integration test kernel/db/pool_test.go runs with session zone America/Toronto.

## Q-003 GateKind has no field for the spec gate id

- Task: T-002L.2
- Sources: spec/kernel/gatekind.schema.json (no spec id field); a2-interfaces.json gate_kinds[].spec_gate; DECISIONS-NEEDED CF-04 ("gate code = slug ... plus a spec id column").
- Reading A: drop the spec id from spec/gates.json and trace by slug only.
- Reading B: add an optional `spec_id` string to the GateKind schema (additive).
- Proposed: B.
- Answer: owner (2026-10-11): add `spec_id` additively and record it here. Done in T-002L.2.

## Q-002 GateKind cannot express dual mode or a sole-owner delay path

- Task: T-002L.2
- Sources: a2-interfaces.json gate_kinds[].dual_mode_min_approvers (2 on G-B4, G-B6, G6, G6c, G11, G11t, G12, G-TSIG, G-TREL); access.json GA9 ("two owners, or owner plus 24h delay for a sole owner") and GA11 (sole owner: identity check plus 24h delay).
- Reading A: the gate engine reads a firm setting (dual mode) and raises min_approvers at runtime; spec/gates.json carries the base value.
- Reading B: add `dual_mode_min_approvers` and a sole-owner `cooling_off` path to the GateKind schema.
- Proposed: A for dual mode (firm setting, T-007/T-007L). For GA9 spec/gates.json sets 2 approvers, so a sole-owner firm cannot export until the delay path exists. See docs/gates-defaults.md.
- Answer: owner (2026-10-11, as the firm's lawyer): B, with two additive optional GateKind
  fields. `dual_when` (e.g. `"signatories>=2"`): require 2 distinct approvers when the firm has
  at least 2 eligible approvers holding the gate's capability. `sole_owner_delay` (Go duration,
  e.g. `"24h"`): when only one eligible approver exists, that person may approve alone only after
  the delay, with notification. Applied: the nine dual-mode trust gates get
  `dual_when: "signatories>=2"`; GA9 and GA11 get `min_approvers: 2` with
  `sole_owner_delay: "24h"`. Done in T-002L.2.

## Q-T032L1 app/legal register test expects modules to register nothing

- Task: T-032L.1
- `app/legal/register_test.go` `TestRegisterWithNoFeaturesRecordsNothing` asserts that
  `legal.Register` makes zero registry calls. T-032L.1 must register document kinds and
  retention classes, so that test fails (16 calls). The file is outside this task's list.
- Proposed: the integrator replaces the zero-call check with a check that `Register` twice on a
  `registryfake` and `Seal` reports only `ErrDuplicateID`-free results for one call, or drops
  it. Every wave 1 task that registers anything hits the same failure.
- Answer: owner (2026-10-11): same as Q-005. Registration tests assert only the "always" set;
  feature-gated kinds (docs kinds, RC-* retention) register only when their flag is on. Done:
  `docs.RegisterKinds` is exported and not in the always-run registrars, so
  `TestRegisterWithNoFeaturesRecordsOnlyAlwaysSet` stays as is.

## T-035L.2 Court filing contract does not match comms.comm_filing; no registry kind for message templates

- Task: T-035L.2
- Sources: app/legal/comms/contract.go (`Filing`: Court, Reference, DocumentID, status
  draft/filed/rejected, FiledOn); migrations/0001_init.sql `comms.comm_filing` (key
  `(comm_item_id, matter_id)`, `rule`, `confidence_ppm`, status proposed/filed/rejected,
  `filed_by`, `filed_at`): the table files a communication item to a matter, it is not a court
  filing. `comms.message_template` is versioned (`key`, `version`, `channel`, `body_ref`,
  approval columns) while the contract `Template` holds Subject and Body inline. The task says
  templates are "registered by the app", but `kernel/registry.Registry` has no message-template
  kind (only EventType, DocumentKind, PortalPage, ...).
- Reading A: comm_filing is "file this message to a matter" (agent proposes with rule and
  confidence, a user confirms). The contract `Filing` gets reshaped additively to match and a
  court-filing record moves to a later task with its own table.
- Reading B: court filings are a new additive table (e.g. `comms.court_filing`) matching the
  contract; comm_filing stays as is for message-to-matter filing.
- Templates: the app either seeds `comms.message_template` rows at boot through its own
  store, or `Registry` gains an additive `MessageTemplate` kind.
- Proposed: B for filings (contract already describes court filings), and app-side seeding
  for templates (no Registry change).
- Answer: owner (2026-10-11): B. A separate additive `legal.court_filing` table (court,
  reference, draft/filed/rejected); `comms.comm_filing` stays as message-to-matter filing. The
  app seeds its own message templates (versioned `body_ref`) at registration; no registry kind
  is added. T-035L.2 is requeued.

## AI-008: /healthz model field

- No `/healthz` handler exists on main yet, and AI-008 lists no server file. AI-008 ships
  `ai.Config.Check(env)` and `httpclient.FromConfig(...)`, whose `ModelClient.Mode()` is the
  value `/healthz` must report as `model: mock|live`. Proposed: the task that adds `/healthz`
  (kernel/server) reads `Mode()` from the client built at boot, and boot passes `COUNSEL_ENV`
  as `env` so a live config in test or ci fails with `ErrLiveModelForbidden`.

## Q-FLAGS-001 Verify flags that modules define but app/legal does not register

- Tasks: T-041.x (export), T-044.x (reports), T-037L.x/T-038L.x (clio), T-035L.1 (conflict)
- Sources: `export.FlagPostCancelWindow` and `reports.FlagBillingRecordsVerified` were read
  with `flags.Enabled` but missing from `app/legal/flags.go`; several packages repeated
  flag names as private copies.
- Answer: owner (2026-10-11): the integrator registers every new flag in app/legal/flags.go,
  default off. Done: `billing.records_verified` and `export.post_cancel_window` added
  (`conflict.id_verification` and `trust.bylaw9_verified` were already there). Module packages
  cannot import app/legal (it imports them), so each keeps one exported mirror per package;
  the private copies in clio and billing/assessment were folded into it, and
  `TestFlagMirrorsMatchCanonicalNames` checks every mirror against app/legal.

## Q-GL-DIM: generic line dimensions on kernel/ledger.Line

- `gl.journal_line` carries optional columns beyond the core ones (`party`, `tax_code`,
  `bank_line_id`, and the app's `matter_id`). Reverse copied only the three kernel ones, so a
  reversal lost the app's link; the kernel may not name it.
- Done (additive): `ledger.Line.Dimensions map[string]string`, keyed by column name. Post
  writes any listed column the table has (unknown name is `ErrInvalidEntry`); Reverse reads
  the optional columns from the catalog and copies every one unchanged, with sides swapped.
  Post also calls `CheckOpen`, so a closed period is `ErrPeriodClosed` before any write.

## Q-JCS-FLOAT: canonical JSON numbers without floats (resolved)

- Resolved: no float formatting. Gate payload numbers are integers within +-(2^53-1), and
  the ES6 form of such an integer is its decimal form, so the one canonical writer
  (`kernel/canon`) formats numbers with integer math only and rejects a fraction, an
  exponent or an out-of-range integer (`canon.ErrNumber`). `kernel/gate` (`JCS`,
  `PayloadHash`, API unchanged) and `kernel/ai/fixture` (`PromptHash`) both use it, and
  neither is in `tools/nofloat_allow.txt`. RFC 8785 Appendix B float vectors are tested as
  rejected inputs.

## Q-INTAKE-PARTY: intake/party import cycle

- intake imports party (Leads create party contacts); party/merge.go imported intake for
  `Submission` and `Lead`. Done: party declares two small interfaces, `party.Submission`
  (`SubmittedContactName`) and `party.Lead` (`LeadContact`), which `intake.Submission` and
  `intake.Lead` implement (additive methods in `app/legal/intake/party.go`). party no longer
  imports intake.
## Q-T-007.3 Decide inputs the gate schema does not hold

- Task: T-007.3
- Sources: migrations/0001_init.sql (gate.approval_request stores no payload; gate.gate_decision.decision allows only approve/reject/changes_requested; core.authority_limit.kind values are app terms); ARC-I-016, ARC-I-020.
- Done for now: `PG.SetDecideHooks(DecideHooks{Preparers, Payload, LimitKinds})` in decide.go supplies editors of the underlying item, the payload (re-hashed against payload_hash) and the gate kind -> authority_limit kind map. The hooks sit in a package-level map keyed by *PG, because the PG struct lives in open.go (not this task's file).
- Proposed: (1) move the hooks into a `PG` field at integration; (2) an approval above the limit returns `ErrRecommendOnly` and stores nothing, since the decision CHECK has no 'recommend' value and the trigger counts every 'approve'. Recording recommendations needs an additive table or decision value.
## Q-T035.4 PortalPage.Capability is registered but not enforced

- Task: T-035.4 (kernel/portal page registration)
- Source: `PortalPage.Capability` says "capability the guest's grant must carry", but
  `portal.Guest` (frozen contract) has only `Grants []string` (subject ids), so the shell
  has nothing to check the capability against.
- Done: `RegisterPage` requires a module-prefixed capability (portalpage schema); `Load`
  enforces the session and the subject grant only.
- Question: add `Guest.Capabilities []string` (additive) so `Load` refuses a page whose
  capability the guest does not carry, failing closed when the list is empty?
## T-011.2: trust book posting and files beyond the task list

- Files: added `app/legal/trust/receipt_test.go` (unit tests against an in-memory store, as
  the owner asked) and this entry; neither is in the task's file list.
- Trust book: the migration header (CF-02) says `trust.trust_transaction` is the book of
  record, TR-1500/TR-2500 are derived from it and `gl.*` holds the operating book only, and
  the `gl.journal_entry.source_type` CHECK has no trust kind. The task asks to post
  TR-1500/TR-2500 through the GL, so `LedgerStore` takes a `ledger.Ledger` for the trust book
  (book `trust`, source kind `trust_transaction`, source id = txn id, TR-1500 lines carry the
  trust account subject and TR-2500 lines the client ledger). Proposed: wire a trust-book
  ledger, or approve an additive migration adding `trust_transaction` to the source_type
  CHECK plus seeding TR-1500/TR-2500 in gl.account. Until then the integration test uses
  ledgerfake for the trust book and checks BIL-I-009 on the derived views
  (`LedgerStore.CheckJournal`).
- `Balance.AvailableCents` subtracts authorized-not-executed withdrawal holds (ARC-I-025),
  which the contract comment ("Cleared - Protected - Minimum") leaves out. Proposed: amend the
  comment additively.
- Flag: nothing in this task is marked (verify), so no code sits behind
  `trust.bylaw9_verified`; the balance checks always apply.

## T-012.2: withdrawal effect files and split with T-012.3

- Files: added `app/legal/trust/withdraw/effect_test.go` (unit tests on trustfake and an
  in-memory store, as the owner asked) and this entry; neither is in the task's file list.
- `Effect.Apply` is the trust.withdrawal EffectFunc (authorisation by a signatory, never the
  preparer or an agent, balance re-checked). `Effect.Execute` posts the disbursement through
  `Disburse` (ledger lock + re-check) when a different user records execution, with the
  sole-practitioner exception audited. T-012.3 (execute.go, RecordExecution) should call
  `Effect.Execute` rather than post on its own.
- `Apply` re-checks with `Ledgers.Balance`, which reads outside the caller's tx; the locked
  re-check happens at `Execute` and in the DB guard (DM-064). Proposed: an additive
  `Ledgers.BalanceTx(ctx, tx, id)` if the authorisation check must run under the lock too.
- Branch: task/T-012.2 is cut from task/T-011.2 (it uses `trust.LedgerStore`); merge T-011.2
  first.
## T-024.1: interest start day (day after months_after, or months_after itself)

- The frozen contract (`Calendar.AccrualStart`) and the T-024.1 accept line start interest on
  the day after `months_after(delivery, 1)` (delivered 2026-01-15 -> accrues from 2026-02-16).
  BIL-C-073 and BIL-L-008 say interest accrues for days `>= months_after(d, 1)` (2026-02-15).
- Done: `interest.Start` follows the contract and accept line, tagged (verify), behind
  `billing.solicitors_act_verified`. Counsel must confirm which day is correct.
- Integrator: add `interest.FlagSolicitorsActVerified` to `TestFlagMirrorsMatchCanonicalNames`.

## Wave 2a integration: additive contract changes (decided)

- app/legal/billing/draft contract: new sentinel `ErrInvalidReduction`. A reduction must be >= 0;
  a write-up is its own explicit action (T-017.5 decision). Additive.
- kernel/docs contract: the `Citation.QuoteHash` comment now states the ARC-I-037 form
  ("sha256:" + hex SHA-256 of the normalised quote). docsfake hashes the same way. Comment only.
- Resolved at wave 2a integration: interest runs from `months_after(delivery, 1)` itself
  (Solicitors Act s.33). The contract comment, `Months.AccrualStart` and tests now say so.

## T-014.2: place of supply (additive contract change; decisions to confirm)

- Additive change to the frozen `app/legal/billing/tax` contract: `Supply` gains
  `PropertyProvince`, `CourtProvince`, `RecipientResident`, `RecipientBusiness`, `MetInCanada`,
  `AgentRole`, `RegisteredProvinces`; `Line` gains `Label` ("RST" for MB PST). Existing fields
  and callers are unchanged. `RecipientBusiness` is not in the T-014.2a accept list: s.23(a)
  zero-rates a non-resident business in Canadian litigation but taxes an individual, so the
  engine must know which. Its zero value (individual) is the taxed, fail-closed side.
- T-014.2a sets `Line.Label` in `compute.go` (not in its file list): Compute is the only place a
  line is built.
- Order (verify): BIL-T-010 and SOR/2010-117 s.27 (Div. 5 applies despite Divs. 2-3) put
  litigation before real property, so a court province beats a property province. The
  T-014.2b accept line lists property first; the engine follows BIL-T-010.
- Fail closed (verify): non-resident treatment needs `RecipientResident == false` and a
  non-Canadian `ClientProvince` code (foreign address on file, BIL-T-015 enforcement). A
  recipient that is not flagged resident but has a Canadian address is taxed by that address;
  no address at all is `ErrUnknownProvince`. Counsel to confirm.
- Rows marked (verify) in b2 (BC/MB/SK/QC places, federal courts, agent role) return
  `ErrUnverified` while `tax.verify_rules` is off.
- T-014.2c and T-014.2d each replace a fail-closed placeholder in `pos.go` with a call into
  their own file (pos.go is not in their file lists).
