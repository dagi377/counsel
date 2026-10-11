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

## Q-DRAFT-001 Draft rules enforced only in draftfake (gap record)

- Task: T-017.2 (BIL-C-021), T-017.3 (BIL-C-041), T-017.4 (BIL-L-002)
- Sources: review L3; `app/legal/billing/draft/draftfake/fake.go` was the only code tagged
  `// rule:` for these ids. BIL-C-022 and BIL-C-023 are already tagged in `draft/lines.go`
  and `draft/examples_test.go`.
- Gap: no production code enforces the bill discount total (BIL-C-021), the no-markup-on-agent
  disbursement rule (BIL-C-041) or the itemized-disbursement delivery block (BIL-L-002). The
  fake keeps its logic for callers' tests but no longer carries `// rule:` tags, so a grep for
  rule enforcement no longer counts it.
- Proposed: the three open tasks add `discount.go`, `disbursement.go`, `itemize.go` with the
  tags and `TestBIL_C_021_*`, `TestBIL_C_041_*`, `TestBIL_L_002_*`; then delete the matching
  fake logic.
- Answer: (owner)

## Q-AGENT-002 F-BIL-03 gate kind differs from the real trust withdrawal tool

- Task: T-037L.2 (agent write tools), F-BIL-03 acceptance test
- Sources: the F-BIL-03 acceptance test expected gate `billing.trust_fee_withdrawal` and
  capability `trust.prepare`; `agent.WriteTools` (app/legal/agent/tools_write.go) opens
  `trust.withdrawal` and needs `trust.request`. The fixture input also did not match the real
  `prepare_trust_withdrawal` schema.
- Change made: the F-BIL-03 test now runs the real `agent.WriteTools` through
  `app/legal/agent/agenttest`; fixtures send the real schema and the test asserts the real
  gate kind and capability.
- Reading A: a trust-to-fees transfer is a trust withdrawal; keep `trust.withdrawal`.
- Reading B: add a separate fee-transfer write tool over `billing/trustapply` with gate
  `billing.trust_fee_withdrawal`.
- Answer: (owner)

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

## Q-T-023.2bb No gate kind for the managing-lawyer write-off approval

- Task: T-023.2bb (blocks T-023.2bc)
- Sources: spec/eng/b3-billing-engine.json BIL-C-085 ("G-B5 ... owner role above threshold",
  config writeoff_second_approval_cents 100000); spec/gates.json has only G-B5
  `billing.credit_note` (approver `billing.credit`, min_approvers 1); spec/legal/access.json
  has no managing-lawyer capability. `app/legal/gates/kinds_test.go` requires every registered
  kind to be in spec/gates.json, so the effect cannot open a kind declared only in Go.
- The task says: if no second-gate kind exists, ask for an additive one and stop.
- Reading A: add a gate kind to spec/gates.json, e.g. slug `billing.writeoff_second_approval`,
  spec_id `G-B5`, label "G-B5 (managing-lawyer second approval)", risk high, min_approvers 1,
  ttl 168h, payload `{write_off_id (uuid), bill_id (uuid), amount_cents (integer >= 0),
  first_approver (uuid)}`, approver = a new capability `billing.writeoff_second_approve`
  granted at level approve to the owner role only in access.json. app/legal/gates registers
  it; T-023.2bb opens it (preparer = the G-B5 approver) and T-023.2bc applies it.
- Reading B: no new kind; raise G-B5 to two approvers (dual_when amount > threshold) and check
  the owner role in the G-B5 effect. This needs an amount-conditional dual mode the gate
  engine does not have (Q-002).
- Proposed: A. T-023.2ba already ships `NeedsSecondApproval` and
  `SecondApproval.CheckSecondApprover` (managing lawyer = owner role, resolved by the caller).
- Answer: owner (2026-10-11): A. Additive gate kind `billing.writeoff_second_approval`
  (spec_id G-B5) in spec/gates.json; approver capability `billing.writeoff_second_approve`,
  held only by the owner role. The two approvers are different people, and neither is the
  preparer or an agent.

## Q-T-045.1 Generated API client cannot live in web/src/kernel

- Task: T-045.1
- Sources: spec/tasks.json T-045.1 files (`web/src/kernel/api/client.ts`); AGENTS.md "Where things go" (generated client in `web/src/kernel`); kernel/boundary_test.go `termRoots` includes `web/src/kernel`; api/openapi.yaml is the legal API (`matter_id`, `/trust/...`, `hst_cents`).
- Problem: a client generated from api/openapi.yaml carries matter, trust and HST names, so TestKernelHasNoLegalTerms fails; `tools/kernel_allow.txt` may only shrink.
- Reading A: generate into `web/src/app/api/client.ts` (app layer); `web/src/kernel` keeps only the domain-free runtime (fetch, problem+json, extensible-enum decode, enumLabel).
- Reading B: keep the path and add `web/src/kernel/api/client.ts` as an exemption in kernel/boundary_test.go (scanner change, not an allowlist line).
- Proposed: A. A working generator (tools/gen_client.mjs, python3+PyYAML for YAML, no new npm deps) and ARC-I-002 tests are drafted and pass vitest; they ship once the path is decided.
- Answer: owner (2026-10-11): A. Generate into `web/src/app/api` (the kernel web stays free of legal terms), with a Node YAML parser (`yaml` devDependency), not python3. T-045.1 is requeued.
- Status: done in T-045.1: `tools/gen_client.mjs` writes `web/src/app/api/client.ts`; `yaml` is a web devDependency.

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
- Amended (decided): prompts may hold decimals, so `canon.MarshalLiteral` / `canon.LiteralNumber`
  add a literal-number mode that keeps a number's own digits, normalised (zero sign, leading
  and trailing zeros, exponent form), with no float parsing. `fixture.PromptHash` uses it;
  gate payloads stay on the integer-only `canon.Marshal`.

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

## Q-CLEARANCE-BOOT: no boot place to wire conflict.Clearance with identity capabilities

- Decision (T-029.2): the only `conflict.Clearer` is `Clearance` (gated), and its
  `Capabilities` must come from the identity registry at boot.
- Blocker: there is no runtime boot wiring yet. `app/legal.Register(registry.Registry)` only
  declares kinds, gates and capabilities; it receives no identity service, store or clock, and
  `cmd/counseld/main.go` only parses flags. `kernel/identity` also has no per-user lookup
  (`Capabilities(ctx, userID) ([]string, error)`); `Access` answers only `Can(...)`.
- Question: where does service construction live (a `legal.Wire(deps)` called from counseld, or
  a kernel boot container), and should `kernel/identity` gain an additive per-user capability
  lookup, or should `conflict.Capabilities` be satisfied by an adapter over `Access.Can`?
  Until then `NewClearance` is wired only in tests.
- Answer: owner (2026-10-11): `app/legal/boot` builds Clearance with Capabilities from a kernel
  `identity.CapabilityLookup`. The lookup is generic: it reads the registered roles and the role
  assignments and returns the capabilities a user holds. It fails closed for unknown users.
  `core.*` kinds are registered by kernel/identity's own admin gates in kernel boot. Done in
  BOOT-17: `kernel/identity/capabilities.go` (additive: `CapabilityLookup`, `ErrUnknownUser` for a
  user with no roles, `ErrUnknownRole` for an unregistered role) and `app/legal/boot/clearance.go`
  (`Options.Clearance` builds `conflict.NewClearance(checks, catalog, lookup, clock.Now)` over the
  catalog BOOT-16 keeps). `kernel/identity` is outside BOOT-17's file list; the owner's answer
  names it.

## Q-GL-BOOK Trust book in gl: book column, trust source kind, per-book close

- Task: T-011.2, T-010 follow-up; CF-02.
- Sources: 0001 header ("gl.* holds the operating book only"); By-law 9 (trust records kept
  separately); `ledger.Entry.Book`; `books.BookOperating`, `books.BookTrust`.
- Answer: owner (2026-10-11): additive migration `0400_ledger_book.sql` (kernel/ledger).
  `gl.account.book` and `gl.journal_entry.book` (default `operating`), source kind `trust`
  appended to the `source_type` CHECK, and a per-book close in the new `gl.book_period`
  (book, month). `gl.period` stays and keeps meaning the operating book; `gl.period_closed`
  and the DM-062 trigger check the entry's book. The trust ledger posts with book `trust` and
  source kind `trust`. CF-02 is reread as "operating views show the operating book only":
  `gl.operating_journal_entry` and `gl.operating_journal_line`. The book CHECK is a format
  check, not `IN ('operating','trust')`, because the kernel engine knows no book name (the
  app's chart registers them). This supersedes "a period close covers all books".

## Q-T-007.3-SCHEMA Gate payload bytes and the recommend decision

- Task: T-007.3 (see Q-T-007.3 above).
- Answer: owner (2026-10-11): additive migration `0300_gate_payload_recommend.sql`
  (kernel/gate). `gate.approval_request.payload_jcs` (JCS bytes) and `payload_sha256`
  (= sha256(payload_jcs) = payload_hash, checked in the DB). `gate.gate_decision.decision`
  gains `recommend`; `gate.check_decision` records it and leaves the request pending. Open
  stores the payload; Decide reads the stored payload (the app hook only serves rows opened
  before the migration) and records an approval above the authority limit as `recommend`
  instead of returning ErrRecommendOnly and storing nothing. A recommendation carries a fixed
  reason because `reason` is required for every non-approve decision.

## Q-T-008.4 Gap-free subject_seq on the outbox

- Task: T-008.4.
- Answer: owner (2026-10-11): additive migration `0500_outbox_subject_seq.sql`
  (kernel/outbox): `platform.outbox.subject_seq bigint`, unique per (aggregate, aggregate_id)
  where set. T-008.4 is requeued; its integration test stays on hold.

## Q-T-036.2 Webhook delivery subject, message and acknowledgement

- Task: T-036.2.
- Answer: owner (2026-10-11): additive migration `1400_webhook_delivery_subject.sql`
  (kernel/webhook): `platform.webhook_delivery.subject`, `message_id`, `payload_ref` (the
  outbox row id; no raw payload) and `acked_at`. T-036.2 is requeued.

## Q-T-034.1 Work items on generic subjects

- Task: T-034.1.
- Answer: owner (2026-10-11): additive migrations `1000_work_item_subject.sql` (kernel/work:
  nullable `work.work_item.subject_kind`, `subject_id`) and
  `6100_work_item_subject_backfill.sql` (app/legal/work: backfill `('matter', matter_id)` in
  batches of 10000). The backfill sits in the app block so no legal term enters a kernel
  migration. `matter_id` stays for app/legal. T-034.1 is requeued.

## Q-T-035L.2 Court filing table

- Task: T-035L.2 (see T-035L.2 above).
- Answer: owner (2026-10-11): additive migration `6000_court_filing.sql` (app/legal/comms):
  schema `legal`, table `legal.court_filing` (court, reference, status draft|filed|rejected,
  document, filed_by, filed_on), RLS forced like every matter table. T-035L.2 is requeued.

## Q-T-008.2 Idempotency-Key on PATCH /webhook-endpoints/{webhook_id}

- Task: T-008.2.
- Sources: every `x-external-effect` operation must take `Idempotency-Key`; renewWebhookEndpoint
  had only `If-Match`.
- Answer: owner (2026-10-11): option A. Add the `IdempotencyKey` parameter (additive; listed in
  kernel/contract/additive.txt). T-008.2 is requeued with its draft.

## Migration numbering for the 2026-10-11 additive schema

- The changes above were asked for as one `0002_additive.sql`. migrations/README.md and
  tools/migration_blocks_test.go reserve 0001-0099 for `0001_init.sql` alone and require each
  file in its owning module's block, so they ship as one file per owner: 0300 (kernel/gate),
  0400 (kernel/ledger), 0500 (kernel/outbox), 1000 (kernel/work), 1400 (kernel/webhook),
  6000 (app/legal/comms), 6100 (app/legal/work). The Go package `migrations` embeds them
  in order for counsel-migrate and the integration tests.

## T-003.2: Allocate moved out of money.go

- T-003.1 already shipped `Allocate` in `kernel/money/money.go`. T-003.2 moves the body,
  unchanged, into `allocate.go` (where the contract header says it lives), so it also touches
  `money.go` (deletion only) and this file. New tests are in `allocate_test.go`; the existing
  `TestAllocate` in `money_test.go` is kept as is.

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

## T-019.1d-f: bill approval effect wiring, fees book storage, BN fail-closed

- Fees book storage: 0001_init.sql has no fees book table. `ApproveEffect` writes the row
  through `FeesBookWriter` in the gate tx, but there is no Postgres writer yet. Options:
  (a) an additive `billing.fees_book` table (append-only, voids kept), or (b) treat the
  numbered `billing.bill` rows as the register (bill_guard already refuses deleting a
  non-draft bill and keeps voids) and drop the separate write. The integration test uses a
  test-only table to prove the row commits and rolls back with the gate tx.
- Registration: gate kinds are registered centrally by `gates.RegisterKinds`, so
  `bill.Effects(*ApproveEffect)` returns the `billing.bill_approval` handler (as
  `signatory.Effects` does) instead of a second GateKind registration in bill/register.go.
  Integrator: pass it into the RegisterKinds effects map at boot.
- Integrator: add `bill.FlagBillingRecordsVerified` to `TestFlagMirrorsMatchCanonicalNames`.
- `CheckDeliverable` uses `tax.BNChecker`, which fails closed: while `tax.bn_check_verified`
  is off, a GST/HST registrant cannot deliver any bill (tax.ErrUnverified). Confirm this is
  intended before the deliver route is wired, or turn the flag on once BIL-T-061 is verified.
- BIL-I-008 has its text in spec/eng/b3-billing-engine.json; `CheckNumbering` implements it.
  It also lets a void bill have no number (voided before approval), as the 0001 CHECK does.

## T-043.1c: app.yaml touched beyond the task file list

- Task: T-043.1c
- Sources: ARC-U-005, ARC-U-041; T-043.1c lists quota.yaml and values.yaml only.
- Reading A: the app pods' priority class, node pool and per-container requests/limits live in templates/app.yaml, so the accept lines cannot pass without touching it.
- Proposed: done. app.yaml Deployments get priorityClassName counsel-<cell>-app, the app pool nodeSelector and toleration, and resources from values. No other change.
- Answer: (owner)

## T-043.4b: lock_timeout limit differs between sources

- Task: T-043.4b (also T-043.4c)
- Sources: MIG-061 says lock_timeout = '3s'; ARC-U-018 and the task say 2 s.
- Decision taken (owner instruction, strictest value, as T-043.1f did): tools/migration_lint.py
  accepts lock_timeout > 0 and <= 2s, and the migration Job sets PGOPTIONS lock_timeout=2s.
  Any value that passes also meets MIG-061 (<= 3s). The task's accept line says the lint
  accepts <= 3s; that is looser than the owner's rule, so the lint uses 2s.
- Answer: (owner)

## AI-034.0a: Embed moved out of aifake/fake.go

The task lists only `kernel/ai/aifake/embed.go`. `Fake.Embed` was defined in `fake.go`, and a
method cannot be defined twice, so it was deleted there and rewritten in `embed.go`. `Vector`
stays in `fake.go`; no other change to it.
`tools/nofloat_allow.txt` gains `embed.go` and `embed_test.go`: embedding vectors are float32
by the `ai.EmbedResponse` contract, the same reason `fake.go` is listed.

## AI-034.0b: Embed journal field on aifake.Fake

The Embed journal needs a slice on the `Fake` struct (guarded by its existing mutex), which is
declared in `fake.go`; that one field is the only change outside the task's files.

## Q-INTEREST-CJA: prejudgment rate when no rate was agreed (integrator, wave 3a)

The decision log says interest with no agreed rate should run at the Courts of Justice Act
prejudgment rate for the quarter of delivery. The frozen rules say the opposite: BIL-C-071
("the Act supplies no default rate. Default: off"), BIL-L-009 (the rate must be printed on the
bill) and BIL-L-010 ("the Act now sets no default rate"). Rule ids outrank decisions, so the
integrator did not change the no-rate behaviour (ErrNoAgreedRate stays). It seeded the
quarterly CJA prejudgment table (2020 Q1 to 2026 Q4, from ontario.ca, verify) as
`interest.PrejudgmentRate`, usable as the agreed rate's CJA benchmark. Needed: a lawyer reads
s.33 as amended and either the rules or the decision change.
- Answer (lawyer, 2026-10-11): Solicitors Act s.33 ties interest to the CJA s.127 prejudgment
  rate, so the frozen rule text is incomplete. Rule behaviour stays the default. New flag
  `interest.cja_default_rate` (verify, default off): when on, a bill with no agreed rate uses
  `interest.PrejudgmentRate` for the Toronto quarter of delivery (`interest.EffectiveRate`).
  Answered.

## T-020.1c: bill void effect

- Approval entry: the billing.bill_approval effect posts no BIL-J-003 journal entry yet, and
  kernel/ledger has no lookup by source. `VoidEffect` finds the entry to reverse through the
  `ApprovalEntries` port, which has no Postgres implementation. Options: (a) the approval
  effect posts BIL-J-003 and stores its entry id on the bill (additive column), or (b) an
  additive `ledger.FindBySource(ctx, tx, kind, id)` over the existing source index. The
  integration test posts the approval entry itself.
- Extra file: `void_effect_test.go` holds the unit tests and the route-table guard
  (TestBIL_C_084_OnlyGateVoids), so they run in `make check`; the listed
  `void_integration_test.go` is integration-only.
- Integrator: pass `bill.VoidEffects(*VoidEffect)` into the gates.RegisterKinds effects map
  at boot, as for `bill.Effects`.

## BIL-J-003: bill approval journal entry

- Resolves the T-020.1c approval-entry question with option (b) inside the bill package:
  `ApproveEffect` now posts BIL-J-003 through `Ledger` (book operating, source `bill`/bill id,
  dated the bill date) in the gate tx, and `PGStore.ApprovalEntry` finds it by that source
  for the void. No contract or migration change; `TaxLine` reads `billing.bill.tax_breakdown`.
- Lines posted: Dr OP-1100 total; Dr OP-4900 discount and negative fee adjustments; Cr OP-4000
  per fee line; Cr OP-4010 taxable (own-cost) disbursements; Cr OP-1210 agency disbursements at
  cost (they were debited to OP-1210 when paid, BIL-J-002); Cr OP-2100 (GST/HST) or OP-2110
  (PST/QST) per tax line. Lines and tax lines must equal the bill's frozen totals or the
  approval fails.
- Not yet posted: the WIP relief pair (Dr OP-1290 / Cr OP-1200 at recorded value) and
  write-downs to OP-4900. Both need the time entries' recorded value, which the bill does not
  carry. Until then OP-1200/OP-1290 keep billed time; the pair nets to zero, so AR, revenue
  and tax are right.
- Integrator: `ApproveEffect` refuses to run without `Journal` (PGStore{}) and `Ledger`
  (*ledger.PG); `VoidEffect.Entries` is PGStore{}. The chart (books) must be synced into
  gl.account before the first approval.

## fix/trust-auth-race: withdrawal authorisation locks the client ledger

- Effect.Apply read `Balance` in its own transaction without a lock, so two concurrent
  authorisations could both pass and together overdraw a client ledger (By-law 9). Apply now
  locks the withdrawal, then the client ledger row FOR UPDATE in the gate's tx (the order
  Execute/Disburse use), re-checks `trust.AvailableCents` net of authorised holds and records
  its hold in the same tx.
- Files outside withdraw/: `withdraw.TrustLedgers` swaps `Balance` for
  `LockLedger(ctx, tx, id) (Balance, held, error)`, so `trust.LedgerStore` (receipt.go) and
  `trustfake` gain that method (additive; the frozen `trust.Ledgers` contract is unchanged).

## T-012.5: EFT check wired into the withdrawal effect

- The Form 9A check (eft.go `CheckEFT`) must run before an EFT posts, which happens in
  `Effect.Execute` (effect.go, not in T-012.5's files). effect.go gains an `EFT EFTRecords`
  field and one call to `CheckEFT` before `Disburse`. While `EFT` is nil every EFT execution
  fails closed with `eft_authorization_missing`.
- Follow-ups: a Postgres `EFTRecords` over `form_9a_doc_id`/docs.document; `problemFor`
  (execute.go) should map `ErrNoEFTAuthorization` to 409 `eft_authorization_missing`; the
  trust.transfer gate effect should call `Transfers.Post`; and `sumsSQL` (trust/receipt.go)
  leaves out authorised `trust_transfer` holds that `trust.available_cents` subtracts.

## Q-T-011.4 Trust exception table for NSF reversals

- Task: T-011.4. Sources: b3 BIL-C-065 ("a reversal of a trust deposit always opens a trust
  exception"), task accept line ("trust exception opened"). 0001 has no table to hold one.
- Answer: owner (2026-10-11): additive migration `5500_trust_exception.sql` (app/legal/trust,
  first file in its block): `trust.exception` (kind `nsf`, receipt id, ledger and matter link,
  amount cents, shortfall cents, opened_at/by, resolved_at/by, resolution), one row per
  (kind, receipt), no delete. `migrations/migrations_test.go` lists the new file. ReverseNSF
  opens the row in the reversal's tx; the replenishment's operating leg is
  Dr OP-1100 / Cr OP-1000 with source kind `nsf_replenishment`. Resolving an exception is a
  later task.

## Q-T-011.5 Ledger balance guard mapping

- Task: T-011.5 (BIL-I-010, DM-052, DM-064).
- DM-052 is a DEFERRABLE INITIALLY DEFERRED constraint trigger, so it fails at COMMIT, after
  any insert-level mapping. `ledgerlock.go` adds `LedgerStore.InLedgerTx` and `ledgerCheck`:
  SQLSTATE 23514 whose message starts with `DM-052:` or `DM-064:` becomes
  ErrInsufficientFunds (409 insufficient_trust_funds); deadlocks and other CHECK violations
  pass through unchanged.
- Follow-up for the integrator (receipt.go is outside T-011.5's file list): `insertTxn` maps
  every 23514 to ErrInsufficientFunds, so an unrelated CHECK on trust_transaction (cash payout,
  kind/sign) reads as insufficient funds. Switch it to `ledgerCheck`.
- The 32-goroutine integration test seeds 32 withdrawals, each authorised under its own
  approved trust.withdrawal gate. It is vetted with `-tags integration` and not yet run
  (integration runs are on hold).

## Q-T-031L.4 Limitations Act calculator: additive contract

- Task: T-031L.4 (gap task: the real calculator behind app/legal/limitation).
- Additive to `app/legal/limitation/contract.go`: Assessment fields `DiscoveryPresumed`
  (s.5(2)), `NoLimit` (s.16/s.17) and `ContributionServedDate` (s.18); sentinels
  `ErrUnverified` (flag off) and `ErrSuspended` (open-ended suspension); types `NoLimitKind`,
  `SchedulePeriod` and the `Schedule` lookup hook (s.19, no data yet).
- For the lawyer (all marked (verify) in `app/legal/limitation/statute.go`):
  (a) a Feb 29 anniversary falls on Feb 28 (Legislation Act, 2006 s.89, month-end rule);
  (b) the due date is not moved off a weekend or holiday (conservative; Legislation Act s.88
  may allow the next business day); (c) undiscovered environmental claims cite s.17;
  (d) an s.22 agreement suspends both periods; (e) a Schedule period replaces the basic and
  ultimate periods and ignores suspensions.
- No Assessor or production wiring exists yet, so nothing used the fake outside tests. The
  BOOT task set should construct `limitation.NewStatute(schedule)` as the Calculator.

## Q-BOOT-13-PORTS: trust services need ports no module implements yet

- Task: BOOT-13 (app/legal/boot trust services).
- Gap: on main there is no production `signatory.Service`, `signatory.Directory`,
  `trust.Sealer`, `withdraw.SignatoryCheck`, `withdraw.FirmPolicy` or `withdraw.Auditor`
  (only fakes and test types), and `*ledger.PG` has no `Balance`, so it is not a
  `ledger.Ledger` on its own (`ledger.Balances` has it).
- Decision taken: `trustServices(k, TrustPorts)` builds `trust.AccountStore`,
  `trust.LedgerStore` and `*withdraw.Effect` (store `withdraw.PGStore{}`) from the module
  constructors and takes the missing ports from `TrustPorts`; each nil port fails boot naming
  it. The AccountStore is the LedgerStore's `SignatoryGuard`.
- Needed: tasks for the identity directory adapter, the account-number sealer, a Postgres
  signatory service, the firm-policy reader, the withdrawal auditor and a kernel ledger value
  that joins `PG` and `Balances`. BOOT-16 fills `TrustPorts` from them.
- Answer: (owner)

## Q-PORT-19: the conflict index version must not be a snapshot xmin

- Task: PORT-19 (app/legal/conflict PGSearcher). The first pass reported
  `pg_snapshot_xmin(pg_current_snapshot())` as `IndexVersion`; that moves with any
  transaction in the cluster and says nothing about the name index.
- Answer (owner, answered): add a real conflict index version, a sequence bumped on party and
  alias writes, additively. Done in `migrations/5201_conflict_index_version.sql`
  (app/legal/conflict): `conflict.index_version_seq` and a one-row `conflict.index_version`
  set to `nextval` by a statement-level SECURITY DEFINER trigger on writes to
  `party.contact` names and aliases, `matter.party_role` and `conflict.sealed_prospect` (all
  three change search results). The row is written in the writer's transaction, so a search
  sees committed versions only; it reads the version first, so the hits are never older than
  it. Cost: name writes serialize on that one row.

## Q-PORT-20: cal.deadline cannot hold a kernel deadline without an additive migration

- Task: PORT-20 (kernel/deadline Postgres store). Stopped; no code written.
- Problem: `kernel/deadline.Proposed` is (PackID, PackVersion, RuleID, Trigger date, Due
  date, Subject{Kind, ID}). `cal.deadline` (0001) has none of PackID-as-text, PackVersion,
  RuleID, Trigger date or a generic subject pair. Its link is `matter_id` XOR `lead_id`
  (a CHECK), `title` and a legal `kind` enum are NOT NULL, and the pack is a uuid FK to
  `cal.rule_pack` (whose `province` and `kind` columns are legal). Kernel code may not name
  `matter` in SQL (tools/kernel_terms.txt), so a kernel store cannot satisfy the CHECK.
  Repack also needs the trigger date and rule id to recompute, which the table does not keep
  (`trigger_id` points at `cal.trigger_event`, itself matter-only).
- Proposal (additive, same pattern as Q-T-034.1 work_item subject):
  1. Kernel migration: `cal.deadline ADD COLUMN subject_kind text, subject_id uuid,
     pack_code text, pack_version text, rule_key text, trigger_on date` (all nullable,
     pair CHECKs NOT VALID then VALIDATE), and `cal.deadline_confirmation` unchanged.
  2. App-block migration: a BEFORE INSERT trigger that sets `matter_id`/`lead_id` from
     `(subject_kind, subject_id)` when the kind is the app's matter or lead kind, and fills
     `title`/`kind` defaults (`'other'`) when the kernel leaves them empty; plus a backfill
     of the new columns from existing rows.
  3. PORT-20 then reads/writes only the generic columns; `rule_pack_id` stays NULL for
     kernel-saved rows (the 0001 CHECK allows it when `rule_cite` is NULL).
- Also needed: the Engine contract's Save/Confirm take a tx but Get does not; PGBook.Get
  would run its own InTx. Confirmations/Repack are Book methods, not Engine methods, so
  they would take a tx too (additive, PGBook only).
- Answer (owner, answered): approved. Additive cal.deadline migration with a generic subject
  (kind, id), pack code and version, rule id and trigger date; an app-block trigger sets
  matter_id/lead_id from the subject. Done in PORT-20: `migrations/1100_deadline_subject.sql`
  and `1101_deadline_subject_validate.sql` (kernel/deadline: nullable `subject_kind`,
  `subject_id`, `pack_code`, `pack_version`, `rule_key`, `trigger_on`; pair CHECKs NOT VALID,
  validated in the later file), and `5900_deadline_subject_link.sql` (app/legal/deadlines:
  BEFORE INSERT trigger mapping the pair to matter_id/lead_id and back, `title` from the rule
  key, `kind` 'other'; batched backfill of the pair). No index on the new columns yet: the
  lint needs a separate no-transaction CONCURRENTLY file, which `migrations.Apply` (one
  batch per file) cannot run; add one when Repack volume needs it. `PGBook` takes a tx for
  Save, Confirm, Confirmations and Repack; Get runs its own transaction.

## BOOT progress

BOOT tasks are not in docs/TASKS.md, so their progress is tracked here.

- Merged to main (wave 4a): BOOT-01, BOOT-02, BOOT-03, BOOT-04, BOOT-05, BOOT-06, BOOT-08,
  BOOT-10, BOOT-11, BOOT-12, BOOT-13, BOOT-14, BOOT-15.
- Merged to main (wave 4b): T-009.4, BOOT-07, BOOT-09, BOOT-16, BOOT-17, BOOT-19, BOOT-20,
  BOOT-22, BOOT-23; with PORT-01, PORT-02, FEAT-CORE-01, FEAT-CORE-04, FEAT-TOOL-02,
  FEAT-TOOL-03, FEAT-TOOL-04, FEAT-TOOL-07, fix/w3-gates and fix/w3-legal.
- Applied at integration (logged decisions):
  - `identity.NewAdminGates(gates, store, opts...)` with `identity.WithCoreEffect(kind,
    effect)`: the app supplies handlers for core.* kinds the kernel owns.
  - Kernel boot builds them: `(*boot.Kernel).AdminGates(opts...)` calls
    `identity.NewAdminGates` on the kernel gate engine and `Deps.AdminStore` and keeps the
    result in `Kernel.Admin`. The legal app calls it with `boot.CoreGateEffects(svc)` when it
    builds its effect table, so the core.trust_signatory effect is the kernel admin gates' one.
  - `boot.LimitationStatute(schedule)` attaches `ontario.Holidays()` (embedded
    holidays.yaml) with `WithHolidays`, so the s.88 roll applies; tested on 2027 holidays.
    `boot.LimitationAssessor(schedule, store, deadlines)` builds the Assessor on it.
  - `import.source_sync` is defined in app/legal/flags.go (`legalflags.ImportSourceSync`),
    default off; the renewal job reads it.
  - `report.SlotWorker` takes a `report.Engine`; the app's report.slot worker is
    `report.SlotWorker` on the kernel Engine and DB, and its own copy is gone.
  - `withdraw.PGEFTRecords` has a unit test on a fake DB. Its integration test is still
    to be written (vet only).
  - Kernel boot `Run` now uses the real kernel job adder (gate.run_due, outbox.relay); the
    BOOT-09 stub is gone.
- Merged to main (wave 4c): fix/w3-trust, fix/w3-deploy, fix/migrate-role (migrate role
  expand/contract and wait-version), PORT-03, PORT-04, PORT-05, PORT-06, PORT-08, PORT-09,
  PORT-13, PORT-14, HTTP-00, HTTP-01, HTTP-02. Applied at integration:
  - `Run` builds the outbox relay over the request pool and calls `addNotifyJobs` and
    `addWebhookJobs` after every app's Register.
  - The legal app schedules `trust.withdrawal_release_expired` (PGStore.ReleaseExpired)
    every 15 minutes when gate services are built.
  - `billing.bill_delivery` is in spec/gates.json (unwired); idem problems carry `type`;
    the billing/bill integration helper applies every migration.
- Still open: register the kernel admin kinds (core.role_change, core.user_activation) with
  the admin gates' effects once a Postgres AdminStore exists; build the Assessor from the
  app's Register once limitation ports have production implementations.

## Q-MIG-LINT-SHIPPED Migration lint on migrations/: edits in place on unreleased files

- Review item 19 (migrations) and 18 (deploy). `make migration-lint` and
  `TestMIG_061_ShippedMigrationsPass` now lint every numbered file in migrations/. Only the
  frozen `0001_init.sql` is baselined (`tools/migration_lint_baseline.txt`, two lines: no
  timeouts; it builds an empty database). The lint itself is unchanged except `--baseline` and
  skipping non-numbered files (smoke.sql).
- The lint is per file, so a forward migration cannot make an earlier failing file pass. No
  release has shipped and no cell has run the migrate Job (boot's migrate role is not
  implemented yet), so these unreleased files were edited in place, each with its fix in a
  new forward file:
  - 0300, 0400, 0500, 1000, 1400, 5500, 5600, 6000, 6100: add `SET lock_timeout = '2s'` and
    `SET statement_timeout = '5min'` (RESET at the end).
  - 0300, 0400, 0500, 1000, 1400: VALIDATE CONSTRAINT moved to 0301, 0401, 0501, 1001, 1401.
  - 0400, 0500, 1000, 1400: CREATE INDEX moved to no-transaction files 0402, 0502, 1002, 1402
    (`CREATE INDEX CONCURRENTLY IF NOT EXISTS`).
  - 1400: `payload_ref ... REFERENCES platform.outbox(id)` inline became
    `webhook_delivery_payload_ref_fk ... NOT VALID`, validated in 1401.
  - Forward only: 5501 narrows the trust.exception UPDATE grant to (resolved_at, resolved_by,
    resolution).
- A dev database migrated before this change has no `schema_meta.applied` and would re-run
  0001; recreate it.
- Still open: 6100 is a backfill in a migration (ARC-U-018 says backfills never run there);
  move it to a kernel/migrate backfill job. The migrate Job still uses the app DB secret;
  give it a dedicated migrator role secret.

## Q-HELM-NAMES Cell namespaces use Counsel names (ARC-U-001)

- ARC-U-001 (spec/eng/a1-uptime.html) lists namespaces counsel-<cell>, paploo-<cell>,
  paploo-<cell>-agents. The chart now creates counsel-<cell>, counsel-<cell>-runtime (agent
  runtime) and counsel-<cell>-agents (agent containers), plus counsel-<cell>-agents quota,
  limit range and counsel-<cell>-agent-scheduling. Owner decision (W3 review item 18): no other
  product name in this repo. The spec text needs the same rename; it was not edited here.

## Q-HELM-MIGRATE First install and the migrate Job

- Decision (autopilot): the migrate Job is a `pre-upgrade` hook only. On install it is an
  ordinary release resource (`counsel-<cell>-migrate-install`), because a pre-install hook runs
  before its namespace, service account, priority class and database secret exist. The chart
  fails on any other hook value.
- Every Deployment has a `wait-schema` init container: `counsel-migrate -mode=wait-version`
  waits, with the app credentials, until `schema_meta.applied` holds the newest migration
  built into the image.

## T-009.4: concrete server, two-way OpenAPI check, unserved list (owner decision)

The owner unblocked T-009.4 before every openapi path has a handler. Files outside the task:

- `api/unserved.txt`: the openapi operations no handler serves yet. The owner asked for
  `kernel/contract/unserved.txt`, but its lines are openapi paths with legal terms and the
  kernel term lint (TestKernelHasNoLegalTerms) scans every file under kernel/; the allow list
  only shrinks, so the file sits next to `api/openapi.yaml` instead. It only shrinks; the conformance tests fail when a listed operation is mounted, when it leaves
  openapi.yaml, and when an operation is neither mounted nor listed.
- `app/legal/conformance_test.go`: the full two-way check has to see app routes, and kernel
  code cannot import app/. It mounts every handler on main (kernel inbound hooks, trust
  withdrawal execution) plus every `Registry.Routes` call `legal.Register` makes.
  `kernel/server/conformance_test.go` checks the kernel routes one way.

Decision taken: `server.New` mounts module routes under `server.BasePath` ("/v1", the openapi
server URL path), and `Routes()` reports the full served pattern. Modules keep mounting
openapi-relative paths, as `kernel/inbound` and `withdraw.Execution` already do.

Left open: the second accept line (refuse a route that sets state owned by a gate effect
unless it is the gate decision endpoint) needs a per-route "gate-owned state" marker that no
contract declares yet. Not built.

## HTTP-00: gate-owned-state guard (files outside the task, decisions taken)

- Write declarations live next to the route: `r.Method(m, path, server.Declare(h, keys...))`.
  `server.DeclaredWrites` walks the mounted router (unwrapping `chi.ChainHandler`) and
  `server.CheckWrites` refuses an undeclared POST/PUT/PATCH/DELETE route and any declaration
  naming a `legal.GateOwned` key, except on `server.DecisionRoute`. Kernel mounters that do not
  use Declare (inbound hooks) are declared in `app/legal/conformance_test.go` (`kernelWrites`).
- `kernel/gate/decide.go` (not in the task's files): `runDecidedEffect` now hands the effect
  `WithEffect(ctx, g.ID)`, so "set only by the effect runner" holds. One line.
- `app/legal/register_test.go`: `MethodRoutes` joins the "always" set. Routes always mount;
  each handler fails closed per request (the withdrawal execution route answers 503 until boot
  wiring supplies its dependencies, and trust_rules_unverified while its flag is off). The
  OpenAPI conformance test needs every route mounted with no features on, so routes cannot be
  feature-gated at registration.
- Fakes: `matterfake` (Create, AddMember) and `withdrawfake` (Authorize) gain
  `GuardEffects(tb)`: effect-owned methods fail tb and return `gate.ErrOutsideEffect` outside
  an effect. Other module fakes gain it when their handler task lands.
- Q-HTTP-00 (open): `bill.delivered_at` is effect-owned (openapi deliverBill: "set only by the
  delivery effect on evidence"; 0001 trigger RV1-008 mentions an approved delivery_correction),
  but spec/gates.json has no delivery or delivery-correction kind. It is mapped to
  `billing.bill_delivery`, listed pending and in the test's `unspecifiedKinds`. Needed: add the
  kind to spec/gates.json (additive) or name the existing kind that owns it.
- Mapping choices to confirm: write_off.status=approved -> billing.credit_note (the openapi op
  "Request a credit note or write-off; opens gate billing.credit_note"); matter.team and
  matter.restricted -> matter.widen_access; matter.status=closed -> platform.closing;
  trust_account.signatories -> core.trust_signatory; trust_txn.posted is split into
  `=receipt` (trust.receipt_classification) and `=transfer` (trust.transfer).

## HTTP-01 / HTTP-02: matter routes (decisions taken, open items)

- Routes register a zero-value `matter.HTTP` / `matter.Writes` through `Registry.Routes`;
  every route answers 503 `unavailable` and calls nothing until boot wiring supplies the
  dependencies (same as the withdrawal execution route). The BOOT tasks must inject them.
- GET /matters: one capability check per request (ARC-I-042); walled and restricted matters
  are left out by Query under the caller's RLS session, not by per-row checks. openapi
  declares no `cursor`/`limit` parameters and no 200 schema for /matters; the handler takes
  both (api.FromRequest) and the 200 bodies are not schema-checked.
- POST /matters: the rule wins over the task text. Same key + different body returns 422
  `idempotency_key_reused` (ARC-I-008), not 409. kernel/idem's own problem bodies omit the
  required `type` field of the Problem schema; the handler checks the missing key itself so
  that 400 validates, but the 422/409 bodies from `idem.New` do not (fix in kernel/idem).
- POST /matters opens `matter.opening` with subject `party.contact`/client_id: the matter
  row does not exist until the effect creates it. Needed: the matter.opening effect
  (service gap) and a confirmation of the subject.
- PATCH: plain-field writes go through a new `matter.Updater` port (declared in
  http_write.go, not the frozen contract); its Postgres implementation is a service gap.
  status=open opens matter.opening; closing/closed/archived open platform.closing (verify);
  team and restricted open matter.widen_access. One gated change per request; mixing plain
  and gated fields is 400.

## BOOT-09: Run wiring gaps

- Additive `Config` fields beyond `Zone`: `DirectDatabaseURL` (db.Open validates both DSNs and
  the worker needs the direct one, ARC-U-011) and `MaxWorkers` (default 10). Server and worker
  need `DatabaseURL`, `DirectDatabaseURL` and `Zone`. `Kernel` and `Deps` gain `Notify`.
- `gate.PG` does not implement `gate.Engine` yet (no Get, Withdraw, RunDue), so the production
  server and worker roles stop with `ErrNotImplemented` at the gate engine until it does.
- Registry schemas load from `spec/kernel` relative to the working directory; the images
  must ship it, or the schemas get embedded.
- Reports use a deny-all authorizer and an audit log with no Verify querier until identity
  is wired at boot. No kernel module owns a River job yet, so `addKernelJobs` adds none.
- `TestRunFlags` now runs the import role and declares its flag in `Define`: Run builds
  through BuildWith, which turns flags on before Register (ARC-I-040).

## Q-BOOT-16-FILES: BOOT-16 touches app/legal/boot/boot.go

- Task: BOOT-16. Files listed: `effects.go`, `unwired_gates.txt`, `effects_test.go`.
- Reason: "Register calls gates.RegisterWired" needs a Register hook, and the hook list and
  `Options` live in `boot.go`. The change is additive: `Options.Gates *GatePorts` (nil keeps
  the old behaviour: no app gate kind registered), and `App` builds a `wiring` value that
  Register fills with the gate Catalog so a later step (BOOT-17 Clearance) can read it.
- Wired: signatory (core.trust_signatory is ignored by RegisterWired, the kernel owns it),
  trust.withdrawal, billing.bill_approval, billing.bill_void (bill.VoidEffects, per the
  integrator decision) and the two write-off kinds. CreditPorts come from the caller
  (no Postgres write-off store yet, Q-BOOT-13-PORTS).

## Q-BOOT-22 notification templates: Options.Sender and the app template list

- Task: BOOT-22 (app/legal/boot notification templates).
- Outside the task's files: `app/legal/boot/boot.go` gains `Options.Sender notify.Sender`
  (additive; the accept line names it) and `opts.registerNotify` in `registerHooks`.
- No app module defines kernel notification templates yet (the comms message templates are
  a separate store, seeded by BOOT-23). The app list in `notify.go` holds one template,
  `sync.subscription_renewal_failed`, for the cliosync renewal alert (ids and times only).
  Modules that need notifications add theirs to `notifyTemplates`.
- Answer: (owner)

## Q-BOOT-23 template seed: Start hook and test fake

- Task: BOOT-23 (app/legal/boot seed message templates at Start).
- Outside the task's files: `app/legal/boot/boot.go` `startHooks` now returns `seedTemplates`.
- A kernel with no DB (`kboot.Build` with no deps, used by `Run` today) skips the seed, so
  `TestAppShape`'s empty-kernel Start stays valid; with a DB, a seed error stops Start.
- `commsfake` has no versioned seed (versions live in `TemplateStore.Seed`'s SQL), so
  `seed_test.go` drives the real `TemplateStore.Seed` over a test-local in-memory
  `comms.message_template` DB instead of commsfake.
- Answer: (owner)

## Q-FEAT-TOOL agent handler set (FEAT-TOOL-03, FEAT-TOOL-04, FEAT-TOOL-07)

- The tool tasks say each handler registers itself from init() into "the agent handler set"
  that `agenttest.FromFeature` (FEAT-CORE-14) reads, but no handler set exists on main. Each
  of these tasks adds the same file, `app/legal/agent/handlerset.go` (identical in all three
  branches, so they merge cleanly): `Handler{Name, Build(Deps)}`, `addHandler` (init only),
  `HandlerFor`, `HandlerNames`, typed `Deps`/`DepOf`, and shared helpers (`handlerTool`,
  `visibleTo`, cursor paging, `MatterDocuments`). FEAT-CORE-14/15 should build on it.
- app/legal/docs has no "documents of a matter" query and kernel/docs has no text search, so
  list_documents and search_documents read through a narrow `MatterDocuments` interface
  (document + current version) that the docs module must implement when wired at boot.
- list_work_items: a work item title is user text with no document citation. It is returned
  as an untrusted `agenttools.Quoted` envelope without a source; CheckCitations passes because
  only the "quotes" array needs citations. Matter-scoped items are those whose subject kind is
  "matter".

## Q-GATE-AMOUNT gate amount path, authority-limit kind and editor lookup per kind

- Source: review of waves 2-3, items 4 and 5 (ARC-I-016, ARC-I-020, BIL-C-085).
- Additive to `kernel/registry/types.go` GateKind: `AmountPath`, `LimitKind`, `Preparers`
  (new type `PreparersFunc`). Additive to `spec/kernel/gatekind.schema.json`: optional
  `amount_path` and `limit_kind`. `spec/gates.json` declares `amount_path` on every kind whose
  payload has a `*_cents` field (bill_approval reads `total_cents`) and `limit_kind`
  `trust_withdrawal_max` on trust.withdrawal and billing.trust_fee_withdrawal.
- Decide no longer depends on SetDecideHooks for these checks: the limit and the kind's editor
  lookup run on every Decide. A money kind with no amount path fails registration and Decide.
- Open (verify): core.authority_limit.kind allows only writedown_pct, trust_withdrawal_max and
  practice_area, so bills have no cents cap kind yet; adding one needs an additive migration.
- Write-offs: `WriteOffEffect.Totals` (nil fails closed) sums write-offs on the same bill plus
  the client's other bills in the last 30 days (verify the window with the lawyer). The
  Postgres implementation must lock the bill row FOR UPDATE.

## Q-W3-LEGAL Wave-3 legal corrections (review items 6 to 9)

Changed expectations, each with its citation. Every rule stays (verify) and behind its flag.

- **RCP 7-day period** (`app/legal/rulepacks/ontario/civil-procedure.yaml`). Rules of Civil
  Procedure, R.R.O. 1990, Reg. 194, r. 3.01(1)(b): holidays are not counted only where the
  period is *less than* seven days. The 7-day rule (renamed `short_period_7_days` ->
  `period_7_days`) now counts calendar days, rolling forward only off a holiday last day
  (r. 3.01(1)(c)). Golden dates: 2026-03-02 -> 03-09 (was 03-11); 2026-04-01 -> 04-08 (was
  04-14); 2026-05-14 -> 05-21 (was 05-26); new 2026-05-11 -> 05-19 (Victoria Day roll).
- **Notice of intent to defend.** r. 18.02(1): the notice is delivered within the time for the
  statement of defence, i.e. 20 days after service in Ontario (r. 18.01(a)), not 10 days.
  Golden dates: 2026-03-02 -> 03-23 (was 03-12). New rule `statement_of_defence_after_notice`
  (r. 18.02(2)): a notice delivered in time gives 10 more days, so 30 days from service of
  the claim; trigger `claim_served_notice_of_intent_delivered` is raised only when the
  notice was delivered in time. Golden: 2026-03-02 -> 04-01; 2026-03-04 -> 04-07 (Good
  Friday and Easter Monday roll). Not modelled: 40/60-day service outside Ontario
  (r. 18.01(b), (c)) and r. 3.01(1)(d) deemed-service times.
- **r. 1.03 holidays.** Unchanged: "holiday" includes every Saturday and Sunday, so the pack
  skips both in business counting and the forward roll.
- **Limitations Act, 2002 s.22** (`app/legal/limitation/statute.go`). An agreement suspension
  now reaches the s.15 ultimate period only when it starts on or after discovery. The
  instruction for this fix said "except business agreements under s.22(5) and (6)". The
  statute as I read it says otherwise: s.22(4) lets *any* agreement made after the effective
  date suspend or extend the s.15 period, but only if the claim has been discovered; s.22(5)
  para. 2 lets a business agreement *vary* the s.15 period but says it may be suspended or
  extended "only in accordance with subsection (4)". So no business-agreement exception was
  added (no `BusinessAgreement` field); the discovery condition applies to all agreements.
  The suspension's start date stands in for the date the agreement was made. (verify s.22(3)
  to (6) and the 2006-10-19 effective date.) New tests: agreement after discovery suspends
  both (basic 2024-01-11, ultimate 2035-01-11); agreement before discovery leaves the
  ultimate at 2035-01-01.
- **Legislation Act, 2006 s.87/s.88 roll.** The limitation roll now uses the s.87 "holiday"
  definition: Sunday plus the dated holidays. Saturday is not an s.87 holiday, so a Saturday
  last day is no longer moved (fail closed). Changed: ultimate 2041-09-28 (Sat) stays (was
  09-30); basic 2026-02-28 (Sat) stays (was 03-02). New: a Sunday last day rolls to Monday;
  a Saturday Boxing Day rolls past Sunday to Monday. (verify) Whether a court office closed
  on Saturday extends the time under the Legislation Act's closed-office provision, and
  whether s.87 makes the r. 1.03 substitute Mondays (e.g. 2026-12-28) holidays; the
  calendar passed in is the r. 1.03 list, which includes them.
- **Interest due date** (`app/legal/billing/interest`). Additive contract field
  `interest.Bill.DueDate` (zero = none agreed). `Posting.Post` passes it to `Start.Clip`, so
  interest starts on the later of the agreed due date and one month after delivery
  (BIL-C-070, Solicitors Act s.33). New tests: due 2026-03-20 -> 10 days in March; a period
  ending on the due date -> ErrNotDue; an earlier due date never starts before 02-15.
- **ETA Sch. VI Part V s.23: NOT changed; needs the tax lawyer.** The instruction asked to add
  an "individual in Canada when the service is performed" exclusion and to apply the
  litigation exclusion to all recipients. The provision does not say that: s.23(a) excludes
  "a service rendered to an individual in connection with criminal, civil or administrative
  litigation in Canada, other than a service rendered before the commencement of such
  litigation" (individuals only), and s.23 has no presence test. The presence exclusions
  are in s.7(a)/(a.1), and s.7(b) takes advisory, professional and consulting services out
  of s.7. The frozen rule BIL-T-015 (spec/rules/b2.json) quotes this text and has test
  cases for both points, and rule ids outrank the code. Changing the behaviour would mean
  changing a frozen rule against the statute, so the code and tests are left as they are.
  (verify against the current s.23 text and GST/HST Memorandum 4.5.3.)

## PORT-01 (kernel/gate: *PG implements gate.Engine)

- Branch is cut from origin/main with origin/task/BOOT-09 merged in, because the run.go
  ErrNotImplemented assert this task removes exists only on that branch.
- `kernel/gate/open.go` (outside the task's file list) gains one field on `PG`: `db TxRunner`,
  set with `SetDB`. `Engine.Get` takes no tx, so the engine needs its own read handle; the
  field is additive. Boot's `pgGates` now takes the pool and wires it.
- Withdraw sets `counsel.gate_tx` itself (there is no `gate.withdraw()` SQL function in
  0001); `counsel_app` holds UPDATE on gate.approval_request. A SECURITY DEFINER
  `gate.withdraw(uuid, uuid)` in a later additive migration would be tighter.
- RunDue skips a gate whose effect failed (recorded as failed), whose kind is unknown or whose
  stored payload fails its hash, logging a warning, so one bad gate does not block the rest.

## PORT-02 (kernel/registry/schemas: embedded spec/kernel schemas)

- Branch is cut from origin/main with origin/task/BOOT-09 merged in (run.go lives only there).
  The merge resolves two conflicts against main's wave-4a boot work: Kernel keeps main's
  fields (BOOT-09 had a second `Notify`), and run.go drops its stub `addKernelJobs` in favour
  of main's real one in jobs_kernel.go.
- `kernel/registry/schemas/embed_integration_test.go` is added beyond the task's file list to
  meet the "integration test compiles" line; it needs no database.

## FEAT-TOOL-02 search_contacts

- Additive contract change (approved in the FEAT plan answers): `party.Contacts` gains
  `Search(ctx, query)` and `party.SearchLimit`; Store and partyfake implement it. The Store
  query uses LIKE on lower(legal_name/display_name) with wildcards escaped; it has no
  integration test yet.
- Visibility: a contact is returned only when it is the client of at least one matter the
  acting user can see (matter.Query List by ClientID, then walls). A contact of walled
  matters only is omitted, not refused with ErrNotVisible: the input has no matter id, and a
  404 on a name search would tell the user a walled client exists. Parties other than the
  client (matter.party_role) are not reachable through matter.Query, so they are not found.
- Citations: a contact name is client text with no document version. It is returned as an
  untrusted Quoted envelope cited to the contact record (`contact:<id>`, page 1, line 1) so
  CheckCitations validates it; quotes[i] is the name of contacts[i].
- `app/legal/agent/handlerset.go` is byte-identical to the file on task/FEAT-TOOL-03 (see
  Q-FEAT-TOOL agent handler set), so the branches merge cleanly.

## Q-W3-TRUST Trust review fixes: additive contract changes

- Source: wave 2-3 review, trust items 2, 3, 10 and 11 (By-Law 9).
- `migrations/5501_withdrawal_authority.sql` (app/legal/trust block, additive):
  - `trust.withdrawal_request.authorized_at` / `expires_at` (nullable). `MarkAuthorized` sets
    `expires_at = authorized_at + 72h` (`withdraw.AuthorizationTTL`, the G11 gate TTL).
    `PGStore.Lock` fails closed once it has passed, and on any authorised row with no expiry
    (rows authorised before this migration must be re-authorised). Expired holds are released
    to `cancelled`: on the same ledger at the next authorisation, and by
    `PGStore.ReleaseExpired`, which the boot-wiring task set must schedule as a periodic job.
  - Trigger `withdrawal_terms_frozen`: amount, payee, ledger, method, bill, purpose, preparer
    and receiving account never change once a withdrawal is proposed; editing is a new
    request. `form_9a_doc_id` may be attached once while awaiting authorisation.
  - Trigger `transfer_terms_frozen` on `trust.trust_transfer` once it leaves draft.
  - Table `trust.form_9a_signature` (append-only): the Form 9A signer and signing time.
    `PGEFTRecords` reads the signer from it, never the document uploader; no row, no record.
    `PGEFTRecords.RecordSignature` writes it. (verify the Form 9A signing requirements)
- `spec/gates.json` (additive): `trust.withdrawal` payload gains `withdrawal_id` and requires
  it and `method`; `trust.transfer` gains and requires `transfer_id`.
- `withdraw.EffectPayload` / `TransferPayload` carry the terms (amount, payee, method, matter;
  for transfers amount and both matters). Apply and Execute fail closed on any mismatch with
  the row. Execute re-reads the approved payload through `Effect.Approvals`
  (`PGApprovals`: stored `payload_jcs` of the approved gate); nil means no execution.
- `trust.Receipt.CashGateID`, `payment.Capture.Method/MatterID/CashExceptionCode/CashGateID`
  (additive): an exception code passes the $7,500 cash limit only with an approved
  `trust.cash_over_limit` (G6c) gate for the same matter, amount and code.
  `trust.LedgerStore.CheckOperatingCash` is the operating-path check (same per-matter lock);
  `payment.CheckCash` must be called by the Postgres Capture when it lands.
- Not fixed here: `billing.cash_received_cents` still counts operating cash only through
  allocations (billing.payment has no matter_id); needs its own additive change.

## fix/migrate-role: migrate role, gate verdict order, test role lock

- `counsel-migrate -mode=expand` applies the files without a `-- contract: <release>` mark;
  `-mode=contract` applies only the marked files and refuses while any expand file is
  unapplied (`migrations.ErrExpandPending`). Both record in `schema_meta.applied` and use the
  direct DSN when given (ARC-U-011). A failure exits non-zero.
- 0302 re-creates `gate.check_decision` so an unknown verdict fails as
  `gate_decision_verdict_ck` before any DM rule: a BEFORE trigger runs ahead of CHECK
  constraints, so DM-070 used to win (TestGatePayloadAndRecommend).
- Integration helpers hold `migrationstest.LockClusterRoles` while building their database:
  the frozen 0001 names the cluster roles, so they cannot be unique per package; schema setup
  is serialised instead.
- Still open: move the 6100 backfill into a `kernel/migrate` Backfill step run by a worker job
  (needs a durable Checkpoints store and an app hook for the step), and the migrator role secret.

## PORT-03 (kernel/boot: identity-backed report Authorizer)

- Built on task/PORT-02's merge of origin/task/BOOT-09 into main (run.go lives only there).
- run.go gains an optional `env.identity` (Access + PrincipalResolver from the pool). Production
  leaves it nil until the identity store is wired (BOOT-20), so reports stay on denyAll there;
  `reportAuthorizer` switches to `reportAuth` as soon as an Access is supplied.
- "Firm subject" is the zero identity.Subject (organisation-wide); the kernel names it
  `orgSubject` so no app term enters kernel code.
- There is no Postgres RoleAssignments in the kernel yet; the integration test reads
  core.app_user/core.role through a test-local one.

## HTTP-00: gate-owned-state guard (files outside the task, decisions taken)

## HTTP-03: contacts routes (files outside the task)

## HTTP-01 / HTTP-02: matter routes (decisions taken, open items)

- Routes register a zero-value `matter.HTTP` / `matter.Writes` through `Registry.Routes`;
  every route answers 503 `unavailable` and calls nothing until boot wiring supplies the
  dependencies (same as the withdrawal execution route). The BOOT tasks must inject them.
- GET /matters: one capability check per request (ARC-I-042); walled and restricted matters
  are left out by Query under the caller's RLS session, not by per-row checks. openapi
  declares no `cursor`/`limit` parameters and no 200 schema for /matters; the handler takes
  both (api.FromRequest) and the 200 bodies are not schema-checked.
- POST /matters: the rule wins over the task text. Same key + different body returns 422
  `idempotency_key_reused` (ARC-I-008), not 409. kernel/idem's own problem bodies omit the
  required `type` field of the Problem schema; the handler checks the missing key itself so
  that 400 validates, but the 422/409 bodies from `idem.New` do not (fix in kernel/idem).
- POST /matters opens `matter.opening` with subject `party.contact`/client_id: the matter
  row does not exist until the effect creates it. Needed: the matter.opening effect
  (service gap) and a confirmation of the subject.
- PATCH: plain-field writes go through a new `matter.Updater` port (declared in
  http_write.go, not the frozen contract); its Postgres implementation is a service gap.
  status=open opens matter.opening; closing/closed/archived open platform.closing (verify);
  team and restricted open matter.widen_access. One gated change per request; mixing plain
  and gated fields is 400.
- Q-HTTP-00 (answered, wave 4c merge): `billing.bill_delivery` is added to spec/gates.json and
  the a2 gate_kinds table (additive; G-B2, billing.approve, medium, 168h, effect
  `billing.deliver_bill`, ARC-I-052). It leaves `unspecifiedKinds`; its delivery effect is not
  built yet, so it is listed unwired.
- Wave 4c merge notes: migrations 1401_webhook_expiry_notice and 5501_withdrawal_authority
  clashed with same-numbered files and are renumbered 1403 and 5502; the withdrawal expiry
  index moves to 5503_withdrawal_authority_index.sql (CONCURRENTLY, MIG-061). Run wires the
  outbox relay over the request pool (db.Pool gains plain Exec/Query/QueryRow) and calls
  addNotifyJobs and addWebhookJobs. The legal app schedules
  trust.withdrawal_release_expired every 15 minutes when gate services are built. kernel/idem
  problem bodies carry `type` (urn:counsel:problem:<code>, now api.ProblemTypePrefix).

## FEAT-CORE-02 answers carry citations; explicit tool HandlerSet

- Owner decision (2026-10-11): tool handlers do not register through init(). Outside the
  task's files, `app/legal/agent/handlerset.go` now holds an explicit `HandlerSet`
  (`NewHandlerSet` rejects duplicates, `For`, `Names`) and `Handlers()` lists every real
  handler; the four tool files drop their init(). `HandlerFor`/`HandlerNames` remain as
  wrappers over `Handlers()`, so existing callers keep working. A new tool adds its
  `Handler` to the list in `Handlers()`.
- Additive to the frozen `kernel/agenttools/run` contract: `Spec.AnswerMayBeUncited bool`
  (default false, so an answer must cite). `CheckAnswer` reads `{answer, citations[]}` with
  snake_case citation keys (`version_id`, `page`, `from_line`, `to_line`); `AnswerSchema` is the
  matching `Spec.OutputSchema`. A cited range may be a sub-range of a citation a tool result of
  the same run returned (same version and page).

## FEAT-CORE-06 run scope (gate-kind allowlist and drafts made in this run)

- Additive to the frozen kernel/agenttools contract (new file scope.go): `Scope{RunID,
  GateKinds, Drafts}`, `NewScope`, `WithScope`, `ScopeOf`, `CheckGateKind`, `RecordDraft`,
  `ScopedOpener`, `ErrGateKindNotAllowed` (403 gate_kind_not_allowed), and the kernel
  `submit_for_approval` tool (`SubmitForApprovalTool`, capability `agent.ask`).
- Outside the task's files, each a few lines: `run/runner.go` stores one Scope per run in the
  context (Spec.GateKinds) and maps ErrGateKindNotAllowed to its problem code;
  `tool.go` Registry.Call records a write tool's draft outcome in the scope; `write.go`
  OpenGateTool opens through ScopedOpener.
- Fail closed: an agent principal with no scope for its run id opens no gate. User
  principals (the manual path) have no run scope and are not checked. The existing
  open_gate test now runs its agent cases inside a scope declaring both kinds it uses; its
  assertions are unchanged.
- Drafts are keyed by the write output ref (the subject id); submit_for_approval on any other
  subject is ErrNotFound (404). The gate payload carries subject ids only; the model's note is
  not stored (AI-R-005).

## FEAT-TOOL-21 prepare_trust_withdrawal amount provenance

- Closed by FEAT-FIX-A: `WriteDeps.Provenance` is checked only when set. Now nil fails closed
  (ErrMissingDep, nothing recorded). `agenttest.New` builds one Provenance per driver, and a
  read stand-in whose text is JSON records its figures for the run, as the real read would.
  Boot wiring (BOOT-FEAT-WIRE) must pass the run's Provenance.
- `WriteDeps.Withdrawals` is now the narrow `agent.Withdrawals` interface (Request, Available).
  `withdraw.Service` and `withdrawfake.Fake` satisfy it, so callers are unchanged.
- Closed by FEAT-FIX-A: payee was free text. Now `WriteDeps.Contacts` (the FEAT-TOOL-12
  `MatterContacts` port, required, nil fails closed) and payee must be one of the matter's
  contact ids (`ErrPayeeNotContact`, 422, rule BIL-L-037). The F-TRU-02, F-TRU-10 and
  F-BIL-03 fixtures now pay `agenttest.PayeeContact`.

## FEAT-CORE-10

- Q-FEAT-CORE-10.1: `prompts/rules.md` is embedded and exposed as `feature.Rules()`, but
  `Def.Spec()` (feature.go, FEAT-CORE-08, outside this task's files) still builds
  `System = Preamble(id) + prompt`. Proposed one-line change at merge: `Preamble(id) + Rules() +
  "\n" + prompt`. `feature.PromptHash` hashes `Spec()`, so it follows that change by itself;
  no fixture pins a prompt_hash yet, so nothing needs rehashing.

## FEAT-TOOL-11 draft_document

- Depends on FEAT-CORE-06 (run Scope), which has no branch yet. The handler takes a
  `RunScope` dep (`Returned` for "citations must come from this run", `RecordDraft` for the
  run's drafts); boot adapts the CORE-06 Scope to it once that lands. On the manual path (a
  user principal, no run) citations are only shape-checked.
- No init(): per the owner decision, `DraftDocumentHandler()` returns the Handler and boot
  adds it to the explicit HandlerSet. Until boot does, agenttest.FromFeature keeps its
  stand-in for draft_document.
- The docs module has no draft API yet; the handler writes through `DocumentDrafts`
  (`CreateDraft`), which app/legal/docs implements at boot.

## FEAT-TOOL-12 draft_email

- No init(): `DraftEmailHandler()` returns the Handler and boot adds it to the explicit
  HandlerSet (owner decision); agenttest.FromFeature keeps its stand-in until boot does.
- No email-draft or matter-contact API exists yet. The handler writes through `EmailDrafts`
  (`CreateEmailDraft`, deliberately without a send method) and checks recipients with
  `MatterContacts` (`MatterContactIDs`: client and parties of the matter); boot wires both.
  submit=true opens `platform.outgoing_email` through `agenttools.GateOpener` with subject
  kind `email_draft`; the caller is the preparer.

## FEAT-TOOL-13 draft_note

- No init(): `DraftNoteHandler()` returns the Handler and boot adds it to the explicit
  HandlerSet (owner decision); agenttest.FromFeature keeps its stand-in until boot does.
- No note-draft API exists yet. The handler writes through `NoteDrafts` (`CreateNoteDraft`)
  and opens `matter.agent_note` through `agenttools.GateOpener` with subject kind
  `agent_note`; the caller is the preparer (an agent's acting-for user).

## Q-PORT-15 write-off store has no table for draft write-offs (answered)

PORT-15 asks for `PGWriteOffs` over `billing.write_off`, adding only a second-gate column in
`6220_write_off_second_gate.sql`. The 0001 table cannot hold what `WriteOffStore` needs:

- `billing.write_off` is the approved record only: `approval_id` is NOT NULL, the row is
  immutable (`write_off_immutable` trigger, UPDATE/DELETE revoked), and `billing.bill_balance`
  subtracts every row's `amount_cents` at once (`bill_not_overpaid_wo` fires on INSERT). A
  draft row would reduce the bill's balance before G-B5 is approved, and `MarkAwaitingSecond`
  could never set a column on it.
- It has no `status`, `prepared_by`, `approved_by` or `approved_at`, which `Lock` must return
  (`ApproveWriteOff` checks the preparer and the draft status).
- Its CHECK needs `fee_cents + dtx_cents + dnt_cents + tax_cents + interest_cents =
  amount_cents`, but `SaveApproved(w WriteOff)` only carries `AmountCents`; the tax part is in
  the `BadDebtAdjustment` that `PostBadDebt` gets, not in `SaveApproved`.
- `WriteOffBills` needs `Bill.ResponsibleUser`, which lives on `matter.matter`; billing may not
  join another module's table.

Proposal (needs approval; all additive):
A. `6220_write_off_draft.sql` adds `billing.write_off_draft` (id uuid PK, bill_id, amount_cents
   > 0, reason, prepared_by uuid, credit_gate_id uuid NULL, second_gate_id uuid NULL,
   created_at), append-only except a one-time `second_gate_id` NULL -> value guarded by a
   trigger. `Lock` = draft row FOR UPDATE LEFT JOIN `billing.write_off` on the same id
   (approved when that row exists; approved_by from the gate decision through the gate
   engine, not a join). `SaveApproved` inserts `billing.write_off` with the draft's id and
   `approval_id` = the G-B5 gate id, `ON CONFLICT (id) DO NOTHING` for idempotency.
B. Bucket split: additive `SaveApprovedSplit(ctx, tx, w, adj)` (or `WriteOff.TaxCents`) so
   `tax_cents` = adj.TaxCents and `fee_cents` = amount - tax; or approve putting the whole
   amount in `fee_cents` (loses the tax split in the row; the journal still has it).
C. `ResponsibleUser`: `PGWriteOffBills` takes a `matter.Query`-style lookup port (matter id ->
   responsible lawyer) instead of joining `matter.matter`.

Answer (owner): approve an additive `billing.write_off_draft` table (status, preparer,
approvers, total). The fee, disbursement and tax split is computed at draft time with
`money.Allocate` in proportion to the bill's outstanding components (largest remainder, then
verified). Only the effect writes the approved row to `billing.write_off`. The responsible
lawyer is read through a matter port, never a SQL join.

Done in PORT-15:
- `migrations/5610_write_off_draft.sql` (billing block 5600-5699; the task text named 6220,
  which is the app/legal/agent block): the draft row with its five-bucket split,
  `credit_gate_id` (G-B5) and `second_gate_id`, each set once, and draft -> approved once,
  guarded by a trigger; no deletes.
- `PGWriteOffs` implements `WriteOffStore` and `WriteOffBills`. `Draft` locks the bill, reads
  its outstanding buckets from billing tables only and fails closed when they do not add up to
  the bill balance net of reserved fee transfers (a trust fee transfer has no bucket split).
  `Submit` records the G-B5 gate id; `SaveApproved` refuses a draft without it and inserts
  `billing.write_off` with `approval_id` = that gate, `ON CONFLICT (id) DO NOTHING`.
- `ResponsibleLawyers` is the matter port `WriteOffBill` reads the lawyer through; nil or an
  empty lawyer fails closed. The matter-side adapter is wired at boot (BOOT-15/16).

## Q-PORT-18 conflict run model does not map onto conflict.check_run / conflict.hit (answered)

PORT-18 asks for `PGRuns` over `conflict.check_run` / `conflict.hit` behind an additive
`RunStore`. The 0001 tables need values the conflict domain never has, so a store would have
to invent regulator-facing conflict records:

- `check_run` hangs off `conflict.conflict_check` (`check_id` NOT NULL, which itself needs
  `trigger`, `settings_version` -> `conflict_settings` and `requested_by`), plus
  `index_version` and `complete` NOT NULL. `Request`/`Run` carry only a matter id, terms and
  the preparer: no check id, no version, no index version.
- `conflict.hit` needs `check_party_id` NOT NULL (a `check_party` row with `kind`
  person/organization and `role`), `matched_kind`, `matched_id`, `match_type` and `score_ppm`.
  `Hit` is `{Term, PartyID, MatterID, Role}`: the term is not a check party, and the match
  kind, match type and score are not known. `Hit.MatterID`/`Role` have no column.
- G2 decisions and hit dispositions live on `conflict.clearance` (approval_id, signed_by) and
  on `hit.disposition*`, which is UPDATEd in place; the in-memory `Checks` keeps both as
  append-only logs. Replacing only the run slice leaves `Dispose`/`decide` needing a run
  lookup by id, which `RunStore{Append, Runs}` does not offer.

Proposal (needs approval; additive):
A. `RunStore` = `Append(ctx, Run) (Run, error)`, `Runs(ctx, matterID)`, `Run(ctx, id)`; memory
   default keeps `NewChecks` as is; `NewChecksWith(s, store, now)`.
B. Additive fields: `Request.CheckID`, `Run.CheckID`, `Run.Version`, `Run.IndexVersion`,
   `Run.Complete`; `Hit.CheckPartyID`, `Hit.MatchedKind`, `Hit.MatchType`, `Hit.ScorePPM`,
   filled by the Searcher (the party index knows them). `PGRuns.Append` refuses
   (ErrInvalid) a run or hit missing any of them, never defaults them; version is
   `max(version)+1` under `SELECT ... FOR UPDATE` on the conflict_check row, and a 23505 on
   UNIQUE(check_id, version) maps to a new `ErrRunExists`.
C. Dispositions and G2 decisions stay in memory in this task; a follow-up maps them onto
   `conflict.hit` dispositions and `conflict.clearance` (or an additive append-only
   disposition table, since `hit.disposition` is updated in place).

Answer (owner): approve the additive domain fields on conflict Run/Hit (check record, party,
match type, score ppm), filled by the searcher. Missing values are refused. Add
RunStore.Get(id).

Done in PORT-18:
- `contract.go` (additive): `Request.CheckID`; `Run.CheckID`, `Version`, `IndexVersion`,
  `Complete`; `Hit.CheckPartyID`, `MatchedKind`, `MatchType`, `ScorePPM` (`Hit.PartyID` is
  `conflict.hit.matched_id`).
- `RunStore{Append, Runs, Get}`, `ErrRunExists`, `ErrInvalidRun` and the optional
  `IndexVersioner` searcher in `check.go`; `NewChecks` keeps the in-memory store,
  `NewChecksWith(s, store, now)` takes `PGRuns`. `clearance.go` reads a run through the store
  (one helper call, needed because the run slice is gone).
- `migrations/5200_check_run_detail.sql` (conflict block): nullable `check_run.prepared_by`,
  `check_run.terms`, `hit.ordinal`, `hit.term`, `hit.party_matter_id`, `hit.party_role`, so a
  stored run reads back whole. `PGRuns` refuses (ErrInvalidRun) a run or hit missing any value,
  a hit whose check party is on another check, and on read a run without a preparer or with a
  gap in hit order. Dispositions and G2 decisions stay in memory (proposal C).

## Q-INTEG-12 Pass 12 integration decisions

- Run scope, one concept (additive): the kernel `agenttools.Scope` (FEAT-CORE-06) now also
  holds the citations the run's tool results returned (`AddCitations`, `Cites`).
  `Registry.Call` records them (`RecordCitations`) after `CheckCitations`. The app's
  `agent.RunScope` port (FEAT-TOOL-11..13) stays as the consumer view; its implementation is
  the stateless `agenttools.RunScopes{}`, which reads the Scope the Runner put in ctx and fails
  closed with `ErrNoRunScope` when the ctx has no scope for that run.
- Provenance (FEAT-CORE-07) keeps its own per-run value set keyed by `Principal.RunID`; it is
  built at registration time (`ReadTool`/`SourcedWrite` wrappers), before any run exists, so
  moving it into Scope is not a natural fit. Open: the Runner should call `Provenance.Forget`
  when a run ends (boot task).
- `agent.Handlers()` now lists draft_document, draft_email, draft_note and the
  `ReportHandlers()` pair. No init() registration.
- Q-FEAT-CORE-10.1 applied: `Def.Spec()` builds `Preamble(id) + Rules() + "\n" + prompt`.
- `PGWriteOffTotals` reads pending write-offs from `billing.write_off_draft` (status draft
  with a credit gate set), never the kernel gate table. A submitted draft whose gate was
  rejected stays `draft` and still counts; that only raises the total toward the second
  approval (fails closed). Open: add a `rejected`/`withdrawn` draft status set by the gate
  outcome so rejected drafts stop counting.
- Migrations: PORT-19's `5200_conflict_index_version.sql` renumbered to 5201 (collided with
  PORT-18's 5200). 5200 and 5610 brought to MIG-061 (timeouts, NOT VALID constraints); the
  hit ordinal unique index moved to `5202_check_run_detail_index.sql` (CONCURRENTLY).
- Not done in pass 12 (requeue to the BOOT/PORT task set): filling `feature.Set` at boot and
  building the registry from `agent.Handlers()` (app/legal/boot has no agent wiring yet); the
  report_step step-id map in the PlanStore (needs a step id on `agenttools.Step`, a kernel
  contract change; PORT-25); the matter-side `credit.ResponsibleLawyers` adapter at boot; and
  write-off drafting on bills with trust fee transfers (split the fee transfer first, PORT-15
  note). Q-INTEG-12.1 (lawyer): when a bill was paid partly by a trust fee transfer, is the
  write-off split computed on the outstanding balance after the transfer (fee portion only),
  or must the transfer itself be apportioned across fee, disbursement and tax first?
- `party.Contacts` gains `List(ctx, Filter)` in contract.go (additive, as the task's accept
  line asks), so `partyfake` gains `List` too. Pages run by id (uuidv7, so creation order);
  `ListLimit` is 201, one more than the openapi page maximum, so the handler can see a next page.
- `ErrDuplicate` (409 duplicate_contact, proposed code) maps a unique violation on insert;
  party.contact has no unique key yet, so it fires only once one is added.
- A malformed contact_id answers 400 validation_failed after the capability check (accept
  line), unlike GET /matters/{id}, which answers 404.
- app/legal/boot TestRegisterNoFlagsRecordsOnlyAlwaysSet failed on HTTP-02 merged with main
  (main's test predates the matter routes): Routes calls join the "always" set, since
  HTTP-00 mounts every handler route unconditionally (unwired routes answer 503). The exact
  comparison with legal.Register's calls is unchanged.

## HTTP-04: time-entry routes (files outside the task)

- Additive contract changes in timekeeping/contract.go: `TimeEntry.RowVersion` (the ETag) and
  `Entries.List(ctx, EntryFilter)`; timekeepingfake implements both. No Postgres `Entries`
  exists yet (service gap), so the routes answer 503 until boot wires one.
- `time_entry_id` is a plain string in openapi; ids outside `[0-9A-Za-z-]{1,64}` are 400.
- ErrOutOfScope maps to 422 activity_out_of_scope, but the service flags out-of-scope
  paralegal entries instead of refusing them (BIL-L-046), so no route returns it today.
- A reused Idempotency-Key with a different body answers 422 (rule ARC-I-008, kernel/idem),
  not the 409 in the task text, as in HTTP-02.

## HTTP-05: expense routes (approved scope)

- No expense service existed; the owner approved a minimal one. `timekeeping/expense.go` adds
  `Expense`, `Expenses` (Record, Get, List) and the Postgres `ExpenseStore` on
  timekeeping.expense (draft rows only; approval and billing set the other statuses). Boot
  wiring supplies the store; until then the routes answer 503. Its integration test is on hold.
- An agent principal never records an expense (403), as with time entries.
- `paid_from: trust` is accepted as a draft record only (the DDL allows it while draft); the
  trust transaction link and any trust movement stay with the trust gates.
- A reused Idempotency-Key with a different body answers 422 (rule ARC-I-008, kernel/idem),
  not the 409 in the task text, as in HTTP-02 and HTTP-04.

## HTTP-06: bill read routes (files outside the task)

- Additive contract changes in billing/bill/contract.go: `Service.List(ctx, Filter)`, `Filter`,
  and the stored fields the openapi Bill and BillLine require (`Bill.Version`, `PeriodFrom`,
  `PeriodTo`, `TaxRatePPM`, `TrustAppliedCents`, `BalanceCents`, `CreatedAt`; `Line.ID`,
  `Line.Taxable`). billfake implements List and bumps Version on each write. No Postgres
  bill.Service exists yet, so the routes answer 503 until boot wires one.
- Stored statuses map to the openapi BillStatus: preflight_failed -> draft,
  pending_approval -> in_review, void -> voided; written_off and credited pass through
  (x-extensible-enum). `status=returned` matches nothing: no stored status is "returned".
  Delivery channels mail and hand show as "print".
- List pages use the openapi `{data, next_cursor}` shape. Bills on matters hidden from the
  caller are left out of the list (pages can be short).
- Line actions are not stored on bill.Line yet; every line reports action "bill".

## HTTP-07: bill draft routes (files outside the task)

- No single-draft entry point exists (billing/draft has Calc+Store, billing/batch builds
  drafts), so http_draft.go adds the narrow port `bill.Drafts` (Create, Edit) with
  `DraftRequest`, `DraftEdit`, `LineAction` and the sentinels `ErrNotEditable` (409
  bill_not_editable) and `ErrNoUnbilledWIP` (422 no_unbilled_wip). Boot wiring must supply a
  Drafts built on billing/draft; until then the routes answer 503.
- PATCH edits only stored draft statuses (draft, preflight_failed). A bill in review is 409;
  there is no stored "returned" status yet.
- A body carrying status, number or totals is 400 validation_failed (accept line), not the
  422 read_only_field in the BillPatch description.
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008, kernel/idem).

## HTTP-08: bill submit, void and deliver routes

- Submit opens billing.bill_approval (202, Gate body) and never approves; void opens
  billing.bill_void. Neither route changes the bill row: no service method moves a draft to
  pending_approval yet, so the status stays draft until a submit service exists (follow-up).
- Submit has no request body in openapi, but the approval payload requires
  delivery_method. The route accepts an optional `{"delivery_method": email|portal|mail}`
  body, default email. Please confirm or add the field to openapi (additive).
- Void requires billing.approve (the requester must hold the approving capability);
  submit and deliver require billing.draft.
- Deliver emits one outbox message `billing.bill.delivery_requested` and returns 202 with
  the bill; delivered_at stays unset until the delivery effect records evidence. The
  consumer that calls bill.Service.Deliver is a boot follow-up.
- Gate subject kind for bills is `billing.bill` (bill.SubjectBill).
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008, kernel/idem).

## Q-FEAT-CORE-12 screen-action route (additive OpenAPI operation)

- Owner approved the additive screen-action operation (FEAT plan answers, 2026-10-11). Proposed
  addition to `api/openapi.yaml`, not yet made (the task does not list that file):
  `POST /api/agent/actions/{feature_id}`, body `{screen: string, record_ids: string[1..100]}`
  (no other fields, so no free text reaches the run). Responses: 200 `{answer, citations[]}`
  (chat features, run inline, nothing persisted; citations use the `run.AnswerSchema` shape),
  202 `{run_id}` (every other feature, queued), problem+json 400, 401, 403
  (`missing_capability`, rule ARC-I-036), 404 (unknown feature or a walled record), 422
  (screen not an action of the feature, or bad record ids), 503 (model busy, Retry-After).
- `app/legal/agent/trigger.Actions` serves it. Boot wires `Screens` (screen id -> subject kind
  and read capability), `Access` (kernel identity), the runner and an `ActionQueue` that inserts
  the ids-only run job. The route is not mounted until the OpenAPI operation lands, so the
  route conformance test stays green.
- Branch base: task/FEAT-CORE-11 (shared `SubjectRef`) with task/FEAT-CORE-02 (`CheckAnswer`)
  merged in; both are on top of task/FEAT-CORE-08.

## FEAT-TOOL-05 list_deadlines

- Additive contract change (named in the task's accept text): `kernel/deadline` gains
  `Filter` and a separate `Lister` interface (`List(ctx, Filter)`), so `Engine` and its
  implementations are unchanged. deadlinefake implements it; the Postgres store has no List
  yet and must implement `Lister` before boot wires list_deadlines.
- Visibility: a named walled matter is refused with ErrNotVisible (404). Without a matter id,
  deadlines of walled matters are omitted, and deadlines on a subject that is not a matter
  are omitted too (walls cannot be checked on them: fail closed).
- The result carries ids, rule references and dates only, no document or client text, so it
  has no quoted envelope. Due dates are YYYY-MM-DD strings, which Provenance.ReadTool records.
- Registered through `ListDeadlinesHandler()` for the boot HandlerSet, not init().

## FEAT-TOOL-06 compute_deadline

- "Published pack": kernel/deadline has no publish state, so the tool reads a new
  consumer-side `agent.PublishedPacks` (wired at boot; a pack still behind its flag, such as
  the unverified Ontario pack, must not be listed). The tool computes only on a published pack
  and refuses with ErrUnknownPack when the engine holds another version than the published one.
- `rule_id` is a bare rule id found in exactly one published pack, or `<pack>/<rule>`; a bare
  id in several packs is refused (invalid input) rather than guessed.
- Walls: the a2 input has no matter id and the computation reads no matter (empty subject,
  nothing saved), so there is no walled-matter case; the accept line on ErrNotVisible does not
  apply.
- No write: `Compute` only; `Save`/`Confirm` are never called. Result has no document or client
  text, so no quoted envelope; due dates are YYYY-MM-DD and recorded by Provenance.ReadTool.
- Not yet in `agent.Handlers()`: `ComputeDeadlineHandler()` is added there (and to its test)
  when boot wires PublishedPacks.

## FEAT-TOOL-08 list_unbilled

- `timekeeping.Entries` has no list by matter and carries no amounts, so the tool reads a new
  consumer-side `agent.MatterTime` (wired at boot over timekeeping entries priced with
  timekeeping/rates, as reports.WIPReporter prices WIP). No timekeeping contract change.
- Unbilled = state approved, billable, no BillID (b3 WIP). `period_to` is inclusive on the
  work date read in America/Toronto. Amounts and the total are int64 cents; a negative amount
  or an overflowing total fails closed. Provenance.ReadTool records every amount and the total.
- The narrative is client text: returned as an untrusted Quoted envelope cited to the entry
  record (`time_entry:<id>`, page 1, line 1), as search_contacts does for contact names.
- Not yet in `agent.Handlers()`: `ListUnbilledHandler()` is added there (and to its test)
  when boot wires MatterTime.

## FEAT-TOOL-09 get_bill

- The frozen app/legal/billing/bill contract has no balance: `Bill` carries totals only and no
  module returns what is still owed on a bill. get_bill reads it through a new
  `agent.BillBalances` port (`BalanceCents(ctx, bill.ID)`), to be wired at boot from billing
  payments, credits and write-offs. Proposed fix: an additive balance read in billing.
- Registered through the explicit `GetBillHandler()`, not init().
- An unknown bill id is `ErrNotVisible` (404), the same as a bill on a walled matter.
- Line narratives are quoted as untrusted text cited to the bill line (`bill:<id>`, page 1,
  line = ordinal); a bill has no document version.
- The tool wraps itself with `Provenance.ReadTool`, so the cents it returns are what a later
  write in the run may carry; it needs the run's `*agenttools.Provenance` in Deps.

## FEAT-TOOL-10 get_trust_balance

- withdraw.Available takes a trust account id and a matter id; nothing on main lists the
  accounts that hold a matter's ledger. The tool reads them through a new
  `agent.MatterTrustAccounts` port (`TrustAccountsOf(ctx, matterID)`), wired at boot from the
  trust ledgers, and returns one figure per account plus their sum.
- Available has no history, so an `as_of` other than today (America/Toronto) is refused as
  invalid input rather than answered with today's balance.
- Registered through the explicit `GetTrustBalanceHandler()`, not init(). The result holds ids
  and cents only, so it quotes nothing; it wraps itself with `Provenance.ReadTool` and needs
  the run's `*agenttools.Provenance` in Deps.

## FEAT-TOOL-15 propose_deadline

- Built on origin/task/FEAT-CORE-06 (run Scope) with origin/task/FEAT-CORE-07 (provenance)
  merged in. Registered through the explicit `ProposeDeadlineHandler()`, not init().
- The frozen kernel/deadline `Proposed` has no title and no pack id, and the a2 input has no
  pack. The deadline is saved with rule id, trigger and due dates and the matter subject; the
  title and the source citation travel in the cal.confirm gate payload the confirming person
  sees. Proposed fix: an additive `Proposed.Title` (and pack id in the a2 input).
- `due_on` and `trigger_date` go through `Provenance.SourcedWrite`: an agent may only carry a
  date a read tool returned in the same run (a compute_deadline result, or a date a document
  read returned with its citation). The manual path is not checked.
- The gate kind is checked against the run scope before the deadline is saved, so a run whose
  feature does not declare cal.confirm leaves no proposed deadline behind.

## FEAT-TOOL-16 create_task

- The a2 `create_task` input has `description_markdown`, but the frozen kernel/work `Item`
  has no description field (the `work.work_item.description` column exists). The tool accepts
  the field (it is schema-valid) and does not store it. Proposed fix: an additive
  `Item.Description` in kernel/work, then the tool passes it through.
- The proposed status is config, not a constant: the tool reads the task workflow and its
  proposed status through a new `agent.ProposedTasks` port, wired at boot from work config.
- An assignee walled from the matter is refused as `ErrNotVisible` (404), like the acting user.

## FEAT-TOOL-19 propose_conflict_parties

- The a2 input names a `check_id`, but the frozen conflict Checker runs by matter
  (`Request{MatterID, Terms}`) and has no check entity or check parties. The tool reads the
  check through a new `agent.ConflictCheckParties` port (CheckMatter, ProposeParties, wired at
  boot over `conflict.conflict_check` / `conflict.check_party`), then runs
  `conflict.Checker.Check` over every party name on the check.
- The result is outcome `proposal` with the run id as ref, not `gate`: G2 is decided on the run
  through `conflict.Clearer`, not a kernel gate the runner could park on. The run stays open.

## FEAT-FIX-A trust withdrawal fails closed; draft_bill checks sources first

## FEAT-FIX-A trust withdrawal fails closed; draft_bill checks sources first

- Closed by FEAT-FIX-B: a fee transfer to the firm's own general account (spec trust.json
  "Payee is firm general account (known)") was refused because it is not a matter contact.
  Decision: a separate firm-payee id. See the FEAT-FIX-B entry below.
- draft_bill reads a new `agent.UnbilledWIP` port (matter's unbilled entry ids up to
  period_to) from Deps; boot (BOOT-FEAT-WIRE) must wire it. Every line action source is
  checked against it before `batch.Drafts.Create`, so a bad source creates no draft.

## FEAT-FIX-B firm general account as a fee payee (By-Law 9 s.9(1))

- Decision: `WriteDeps.FirmAccount` (the firm general account's payee id) and `WriteDeps.Bills`
  (`agent.FeeBills`, returns `withdraw.FeeBill` for a bill on the matter) are new required
  ports; nil fails closed (ErrMissingDep). No BillBalances port existed, so FeeBills is the
  equivalent.
- The firm account is a payee only when purpose_category is fees, bill_id is a bill on the
  same matter, the bill is delivered and amount_cents <= its balance due (rules BIL-L-037,
  BIL-L-035). Otherwise `ErrFirmPayeeNeedsBill` (422), `withdraw.ErrBillNotDelivered` or
  `withdraw.ErrExceedsBillBalance`. Every other payee must still be a matter contact id.
- `withdraw.Request` has no bill field, so the bill id is checked by the agent but not carried
  into the trust request. Boot (BOOT-FEAT-WIRE) must wire FirmAccount and Bills; the trust
  module's own ValidateFee remains the authoritative check at authorization.
- `WriteDeps.Provenance` is checked only when set. `agenttest.New` (not this task's file) builds
  WriteTools without one, and its read stand-ins return quoted text with no numbers to observe,
  so a fail-closed nil would break the F-TRU/F-BIL/F-ACC feature tests. Boot wiring
  (BOOT-FEAT-WIRE) must pass the run's Provenance, and agenttest should wrap its read stand-ins
  with `Provenance.ReadTool`; after that, nil can fail closed.
- `WriteDeps.Withdrawals` is now the narrow `agent.Withdrawals` interface (Request, Available).
  `withdraw.Service` and `withdrawfake.Fake` satisfy it, so callers are unchanged.
- The title says "payee from matter contacts", but the accept list does not cover it and there
  is no matter-contact dependency in WriteDeps. Payee is still free text. Open question.

## Q-INTEG-13b Pass 13b integration decisions

- Feature list: `feature.AppSet`/`appDefs` could not list the deadline Defs, because
  app/legal/deadlines imports app/legal/agent/feature (an import cycle). The list moved to a
  new package app/legal/agent/featureset (`featureset.App()`, `appDefs()`), which imports both;
  `go generate ./app/legal/agent/featureset` now regenerates the plugin skills, and
  TestExportSkillsCheckedIn moved with it. F-DL-01/02/03/04/06 are listed; the three tool-use
  ones (F-DL-02/03/06) generated skills under paploo/counsel-plugin/skills. Boot builds its
  feature.Set from `featureset.App()` (BOOT-FEAT-WIRE).
- Event type names in app/legal/deadlines (DL-05, DL-06) are kept: `deadline.trigger_date_changed`,
  `rulepack.published`, `holiday.added`, `filing.receipt_received`, `claim.issued`,
  `service.affidavit_received`. No module or spec/eng/a2-interfaces.json defines these yet. The
  a2 `deadline.changed` event (cal) is a confirmed due-date change, not a trigger-date change, so
  it is not reused. Open: the owning modules (cal, rulepacks, comms) should publish these types,
  or the triggers rebind to whatever names those modules choose.
- Handler set: `agent.Handlers()` now lists every tool handler (list_deadlines, compute_deadline,
  list_unbilled, get_bill, get_trust_balance, propose_time_entries, propose_deadline,
  create_task, propose_conflict_parties, submit_for_approval, draft_bill,
  propose_payment_allocation). Their new ports (BillBalances, MatterTrustAccounts,
  PublishedPacks, MatterTime, SubjectMatters, ProposedTasks, ConflictCheckParties) come through
  `agent.Deps`; until boot wires them, the handler's Build fails with ErrMissingDep.
- scope.go: FEAT-TOOL-15/17/18 each carried the CORE-06 kernel/agenttools/scope.go; the merge
  kept main's single copy (the pass-12 RunScope version). No duplicate definitions remain.
- Name clash: FEAT-TOOL-09 (get_bill) and FEAT-TOOL-17 (draft_bill) both declared `civilDate`
  and test fixtures `billMatter`, `billRun`, `billWorld`. The draft-bill ones are renamed
  (`parseCivilDate`, `draftBill*`).
- 6200_agent_flag.sql (FEAT-CORE-13) gained the MIG-061 `SET lock_timeout` / `statement_timeout`.
- Not done here: the FEAT-TOOL-16 `Item.Description` additive field (follow-up).

## Q-PORT-11: signatory change log, migration number and files outside the list

- Task: PORT-11. The task names `migrations/6210_signatory_change.sql`, but 6200-6299 is the
  app/legal/agent block in `tools/migration_blocks.txt`; trust owns 5500-5599. The file is
  `migrations/5510_signatory_change.sql` (additive: one new table).
- `PGService` satisfying `Service` needs the PORT-10 `Propose`/`Approve` stubs (and their
  `ErrNotImplemented` test) removed from `service_pg.go` and `service_pg_test.go`. Branch is
  based on `task/PORT-10`, so PORT-10 must merge first.
- `signatory.Effects` calls `Service` without a tx. `TxEffects(*PGService)` runs the same
  effect on the gate tx (`PGService.OnTx`) and records the gate id as
  `trust.signatory.approval_id`; boot should register `TxEffects`, not `Effects`. Outside a
  gate, `Approve` records the change id as the approval.
- One change adds or removes exactly one user (the GA6 payload is one user); a bonded
  non-licensee grant stores bond_cents 0 with no expiry and keeps the evidence on the change
  (verify: the real bond amount and expiry need their own input).

## Q-PORT-12: shared signatory validity helper

- Task: PORT-12. "Shared helper in signatory" needs one file outside the task list:
  `app/legal/trust/signatory/validity.go` adds `CanSignTx(ctx, tx, account, user)`, which runs
  PGService.CanSign's query and validity rule in the caller's tx. `withdraw.PGSigners` calls
  it, so the withdrawal gate and the signatory list use one rule. Branch is based on
  `task/PORT-10`, so PORT-10 must merge first. Boot wires `withdraw.PGSigners{}` into
  `TrustPorts.Signers` with no new SQL.
- Q-PORT-22 (open): the Clio client (app/legal/clio) has no webhook-subscription renew call,
  so nothing implements sync.SubscriptionRenewer yet. PORT-22 ships only OutboxNotifier.
  Needed: a `Renew(ctx, remoteID) (time.Time, error)` PATCH on the Clio webhook endpoint in
  app/legal/clio/client.go (with cliofake support). Until then BOOT-19 wires the renewal job
  only behind its flag, which is off by default.

## Q-PORT-24-SOLE: sole practitioner signs their own trust reconciliation

- Task: PORT-24-SOLE, answering Q-PORT-24 (decided as counsel): By-law 9 needs a licensee to
  sign the monthly reconciliation and a sole practitioner has no second person.
  `recon.PGSignOff.Sign` lets the preparer sign only when the firm has exactly one active
  licensee (core.app_user, same definition as the signatory directory) and the preparer is
  that licensee. It records audit action `sign_off.sole_practitioner` in the sign-off tx
  through `PGSignOff.WithAudit`; without an audit log the preparer is refused. Every other
  preparer is refused (ErrSelfApproval) and an agent never signs (ErrAgentApproval). Tagged
  `rule: ARC-I-021`.
- The database keeps its own guard: `core.check_sod` (0001) also needs
  `core.firm.sole_practitioner` true and writes its `sod_solo_exception` row, so boot must keep
  that setting in step with the licensee count. Showing the exception in the UI is a separate
  web task.
- PORT-26 (logged, autopilot): `kernel/identity/breakglass.go` is edited outside the task's file
  list, because `NewBreakGlassWith` and the `GrantStore` field must live on `BreakGlass`. The
  in-memory store is the default. `audit.break_glass` (frozen) has no expiry column, so
  `PGGrants` treats a grant as expired once its `core.break_glass_expire` audit event with
  detail `grant_id` exists. The boot audit masker must allow `grant_id` as `audit.KindID`;
  otherwise ExpireDue audits the same grant again on every run. The other option is an
  additive `expired_at` column in the kernel/audit block (needs approval). The table holds
  one subject kind in an app-named column, so boot sets `PGGrants.SubjectKind` and
  `SubjectColumn` (the kernel names no legal term); other kinds are refused.
- PORT-26 expired_at (approved by the owner): additive migration
  `0200_break_glass_expired_at.sql` (kernel/identity block) adds nullable
  `audit.break_glass.expired_at`. `PGGrants` now reads expiry from that column and
  `ExpireDue` sets it, in the caller's transaction, after each grant's expiry audit event; a
  grant it cannot mark fails the run (`ErrGrantNotMarked`) so the audit event rolls back too.
  Expiry no longer depends on `audit.audit_event`, so the boot audit masker need not keep
  `grant_id` for ExpireDue to stay idempotent. Grants expired before this migration have no
  `expired_at` and are audited once more on the first run after it.
- PORT-27 (logged, autopilot): `kernel/portal/auth.go` is edited outside the task's file list,
  because Shell has to use the new `SessionStore` (field `Shell.Store`; NewShell sets an
  in-memory store). The migration is `1500_portal_token.sql` in the kernel/portal block, not
  `6240` (an app block). `core.user_session` has no hash or expiry column, so
  `platform.portal_token` holds the SHA-256 keys of sign-in tokens and session handles, the
  grants and the expiry, and links each session to its `core.user_session` row
  (platform 'portal'). Ending that row (logout or revocation) ends the portal session. Guest
  ids must be `core.app_user` ids.

## HTTP-PAGE list routes return the openapi page shape

- Decided (autopilot counsel): openapi declares every list page as `{data, next_cursor}`
  (next_cursor string or null); `api.Page` encoded `{items, next}`. kernel/api/contract.go
  `Page` keeps its Go fields (Items, Next) so callers compile unchanged, but its JSON tags are
  now `data`/`next_cursor` and a `MarshalJSON` method writes `data: []` for no items and
  `next_cursor: null` on the last page. This is a wire change, not additive; it fixes
  GET /matters, /contacts, /time-entries, /expenses, /credit-notes (and the trust lists on the
  HTTP-10/10b/11 branches, whose tests still decode `items`/`next` and must switch to
  `data`/`next_cursor` when merged). GET /bills (HTTP-06) already used this shape.
- openapi gives no 200 schema for GET /matters, /contacts, /time-entries, /expenses; their
  tests check the whole body against the openapi list page envelope. Declaring those 200
  schemas is an open additive openapi change.

## HTTP-10b trust ledger overdraft fields

- Trust ledger overdraft is never hidden (By-law 9; BIL-I-010, ARC-I-025). GET /trust/ledgers
  always sends `overdrawn` (bool) and `shortfall_cents` (>= 0) on each ledger. openapi `Cents`
  is non-negative, so a negative posted or available balance is reported as 0 with
  `overdrawn: true` and `shortfall_cents` = the amount below zero of the lower of the two; the
  true signed figure is that amount minus shortfall_cents. The web must show `overdrawn` as an
  error state. spec/legal names the By-law 9 no-overdraft rule only in prose (billing.json
  "reg"); the code cites BIL-I-010 from spec/eng/b3.
- Closed by Q-HTTP-10c (the fields are now declared). Was open: the two fields were not yet declared on openapi `TrustLedger`. The
  additive change needs a line in kernel/contract/additive.txt, and any pointer under
  `/components/schemas/TrustLedger` puts the legal term "trust" in kernel/, which
  TestKernelHasNoLegalTerms rejects; tools/kernel_allow.txt is a ratchet that may not grow
  (TestKernelRatchet). `TrustLedger` has no `additionalProperties: false`, so the bodies still
  validate. Proposed: exempt kernel/contract/additive.txt (openapi pointers, not kernel code)
  from the term scan, then add `overdrawn: {type: boolean}` and
  `shortfall_cents: {$ref: Cents}` to TrustLedger under this entry.

## HTTP-12: gate inbox and detail routes

- kernel/gate.Engine has no List; the routes take a `gates.Lister` (ListFilter: after, limit,
  states, kinds, not-prepared-by). Boot wires a lister over core.gate under the caller's RLS.
- Reading gates needs at least one kind's approver capability ("any"); GET /gates/{gate_id}
  then needs that kind's approver capability ("the") or to be the gate's preparer. A walled
  subject is 404. The kind check runs after the gate is loaded (one Get call).
- Inbox default (decidable_by_me=true): pending gates of held kinds, minus the caller's own
  (an agent counts as its acting-for user). A malformed gate_id answers 404.
- Each detail view appends one `gate.viewed` audit event in its own transaction.
- `decisions` is always empty: the engine does not expose per-decision records yet.

## HTTP-13: gate decision and withdraw routes

- Decisions need a human principal (agent -> 403 agent_cannot_decide), at least one approver
  capability (403 before any engine call), then the kind's approver capability on the
  subject (re-checked at decision time; walled -> 404). Body `version` and If-Match both
  must equal the gate version (412). The preparer -> 403 self_approval unless approving
  with a sole_practitioner_declaration, which the engine's self-approval policy then decides.
- Step-up comes from `DecideHTTP.StepUp` (when the session last passed MFA). Missing, or
  older than 15 min for high/irreversible kinds -> 401 step_up_required with
  `WWW-Authenticate: Bearer error="insufficient_user_authentication"` (RFC 9470), matching
  kernel/gate's problem table and ARC-I-017's test. An engine ErrMFARequired answers the
  same. Decided in HTTP-13b (the accept line's 403 is superseded).
- Approval runs the effect inside Engine.Decide in the route's transaction; the PG engine
  wraps it in gate.WithEffect. gatefake does not set WithEffect, so the test asserts the
  effect ran once per approval, not the context mark.
- Withdraw by anyone but the preparer (or an agent acting for them) -> 403 not_preparer
  (proposed code). Problem codes self_approval, cooling_off, gate_not_open,
  payload_changed, already_decided, not_preparer are proposed. trust.ErrInsufficientFunds
  from an effect maps to 409 insufficient_trust_funds.

## HTTP-14: payment list, record and get routes (files outside the task)

- `payment.Service` gains `List(ctx, ListFilter)` and `ListFilter` in contract.go (additive,
  as the accept line asks); `paymentfake` gains `List` and an optional `NewID` so tests get
  uuid payment ids. Pages run by id.
- A body with a `trust_ledger` allocation, or a service ErrRetainerToOperating /
  ErrTrustReceipt, answers 422 `use_trust_receipts` (proposed code) with detail and instance
  pointing at /v1/trust/receipts; PaymentCreate has no account field, so "trust-account
  payment body" is read as a trust_ledger target.
- POST /payments captures to at most one bill; more than one bill allocation answers 422
  validation_failed pointing at POST /payments/{payment_id}/allocations.
- Payment has no row version or allocation ids yet: `version` is 1, ETag "1", and each
  allocation id is a stable uuid derived from (payment id, index). received_at echoes the
  business date (midnight America/Toronto), since Capture keeps only ReceivedOn.
  processor_fee_cents is stored and returned since HTTP-14b (below).
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008, kernel/idem).

## Q-HTTP-08b Submit moves the bill and adds delivery_method to the submit body

- `bill.Service` gains `Submit(ctx, tx, id)` and `bill.ErrNotSubmittable` (contract.go,
  additive); billfake implements it. Submit moves a draft to pending_approval and bumps its
  version. POST /bills/{bill_id}/submit now calls it in the same transaction as the
  billing.bill_approval gate open: a failed open rolls the move back, and a bill that left
  draft between the read and the tx answers 409 bill_not_submittable with no gate.
- api/openapi.yaml: POST /bills/{bill_id}/submit gains an optional request body
  `{"delivery_method": "email" | "portal" | "mail"}` (default email, no other fields), the
  value the bill approval payload needs. Optional request bodies are additive under
  ARC-I-001; listed in kernel/contract/additive.txt as
  `/paths/~1bills~1{bill_id}~1submit/post/requestBody`.
- Only billfake implements bill.Service today; the PG bill service (boot follow-up) must
  implement Submit with `UPDATE ... SET status='pending_approval' WHERE status='draft'`.

## Q-HTTP-16 Direct trust receipt posting versus the gate-owned receipt key

- HTTP-16 accept said an unambiguous, under-limit POST /trust/receipts answers 201 and posts
  the receipt in the route, but app/legal/gateowned.go gives `trust_txn.posted=receipt` to
  trust.receipt_classification (ARC-I-012). Options were (A) split the key, or (B) send every
  receipt through trust.receipt_classification.
- **Closed (decided as counsel):** the gate-owned key is not split. Classifying money as trust
  is the control, so every receipt goes through trust.receipt_classification. For By-law 9
  same-day record-keeping, POST /trust/receipts records the receipt at once in the trust
  receipts journal (`trust.receipt_journal`, migration 5520) in state
  `received_pending_classification`, opens the gate with the proposed classification in the
  same transaction and answers 202 with the Gate. Only the gate effect
  (`trust.ReceiptClassificationEffect`) posts to the client ledger. Built in HTTP-16b; the
  route declares `trust_receipt.status=received_pending_classification`, never the gate-owned
  key. Follow-up: boot wires ReceiptHTTP and the effect (gates.RegisterWired).

## REFUND-EFFECT billing.refund gate effect

- Built `payment.RefundEffect` (app/legal/billing/payment/refund_effect.go), the G-B6
  billing.refund effect HTTP-15 needs. Integer cents; the refund never exceeds the payment
  less earlier refunds; replay by gate id records nothing twice; the approver must be a user
  who is not the requester (BIL-C-064).
- Operating refund: records billing.refund (status approved, approval_id = gate) and the
  negative client credit through the `RefundStore` port, and posts Dr OP-2200 / Cr OP-1000
  (gl source_type `refund`, source id = refund id).
- Trust refund: never pays out in the effect. It becomes a trust withdrawal (cheque, payee
  from the payload, prepared by the G-B6 approver so they cannot also authorise it) checked by
  `withdraw.Validate` (bearer payee, overdraw, uncleared funds, By-law 9 flag) and opened
  through the `TrustRefunds` port; the refund row links trust_withdrawal_id, and the trust book
  moves when that withdrawal executes.
- Follow-ups: Postgres `RefundStore` and `TrustRefunds` implementations, and boot wiring of
  `payment.Gated` plus this effect in app/legal/boot/effects.go (gates.RegisterWired). No
  migration: billing.refund already has approval_id, trust_withdrawal_id and status.

## WD-BILL: trust withdrawal names its bill

- Additive in `app/legal/trust/withdraw/contract.go`: `Request.BillID`
  (`trust.withdrawal_request.bill_id`, already in 0001_init.sql, so no migration). The agent's
  prepare_trust_withdrawal passes its `bill_id` on (FEAT-FIX-B). The trust.withdrawal gate
  payload gains `bill_id` (omitempty), so an approval binds the bill.
- `Effect.Apply` re-checks a fee withdrawal at authorization (By-Law 9 s.9(1), BIL-L-035/037):
  it names a bill, the bill is on the same matter, delivered on or before now, and the amount
  is no more than its unpaid balance. A withdrawal with a bill id, or purpose `fees` /
  `fees_to_operating`, is a fee withdrawal. `Effect.Bills` (new port `withdraw.FeeBills`, read
  in the gate tx) nil fails closed. Open (BOOT): wire a billing adapter returning the bill's
  matter, delivery date and balance net of reserved fee transfers.
- Branch is based on task/FEAT-FIX-B (not yet on main) with origin/main merged in; the merge
  resolved the draft_bill test rename (billWorld -> draftBillWorld) and QUESTIONS.md.

## DL-EVENTS: the owning modules publish the deadline trigger events

- Published now, each in the writer's tx through `outbox.Emitter`:
  - `rulepack.published` (`deadline.EventPackPublished`): kernel/deadline `PGBook.Repack`,
    subject `cal.rule_pack`/pack id, body pack id, version and recomputed count. New
    `PGBook.Events` field; nil refuses Repack before any write.
  - `filing.receipt_received` (`comms.EventFilingReceiptReceived`): `FilingStore.RecordFiling`
    for a filing recorded as filed, subject `legal.court_filing`/filing id. New
    `FilingStore.Events` field; nil refuses a filed filing before any tx.
  - `holiday.added` (`ontario.EventHolidayAdded`): `ontario.PublishAddedHolidays(ctx, tx, em,
    known)` emits one event per calendar holiday not in `known`. Boot calls it when it loads
    the calendar, passing the holidays the engine used before.
  - `app/legal/deadlines/events_test.go` pins the bound names to the published constants.
- Open (no write point exists yet, so nothing publishes them):
  - `deadline.trigger_date_changed`: cal has no operation that changes a stored trigger date
    (`cal.deadline.trigger_on` is set once by Save). It belongs to that operation when added.
  - `claim.issued` and `service.affidavit_received`: comms records court filings only; there is
    no issued-claim or affidavit-of-service record to publish from.
- BOOT: set `PGBook.Events` and `FilingStore.Events` to the outbox emitter.

## EVENTS-2: publishers for the remaining F-DL-03 / F-DL-04 events

- Branch carries origin/task/DL-EVENTS (merged in) because it reuses `PGBook.Events` and
  `FilingStore.Events`; merge DL-EVENTS first or together.
- New minimal write points (no schema change), each emitting in the writer's tx and refusing
  before any write when its emitter is nil (DL-EVENTS pattern):
  - `deadline.trigger_date_changed`: kernel/deadline `PGBook.ChangeTrigger(ctx, tx, id, date)`
    updates `cal.deadline.trigger_on` only (due date stays until F-DL-03's proposal is
    confirmed), subject `cal.deadline`/id, body ids plus old and new trigger. Same date is a
    no-op. Not on the frozen `Engine` interface.
  - `claim.issued`: comms `FilingStore.IssueClaim(ctx, filingID, courtFileNo, issuedOn)` sets
    the court file number on a filed `legal.court_filing` row (ErrNotFound otherwise).
  - `service.affidavit_received`: comms `FilingStore.RecordAffidavitOfService(ctx, f)` records
    a filed filing whose document is the affidavit; it emits this event instead of
    `filing.receipt_received`, so F-DL-04 runs once per record.
- Open: no handler or route calls these yet (boot/handler task). Whether an affidavit of
  service received but not filed with the court should also publish (verify).

## Q-BOOT-WIRE-A agent layer at boot

- `Options.Agent` (`AgentWiring`) registers every `agent.Handlers()` tool, the job bindings
  and tick job, the action route, the flag digest template and the FlagSink gate map. Boot
  fails closed: a nil port fails its handler with ErrMissingDep, and `Features()` fails when a
  feature names a tool with no handler or a gate kind outside spec/gates.json.
- Event names chosen from a2 for unbound features: F-DL-01 `document.uploaded`; F-DL-02
  `lead.created` and `matter.updated` (claim facts updated). Checked (BOOT-PORTS-B) against
  spec/ai/features.json triggers ("document filed to matter"; "intake created or claim facts
  updated") and the a2 event catalogue: all three are the a2 events for those triggers and
  stay. a2 had no comms filing event; Q-DRAFT-FIX adds `email.filed`, and F-DL-01 now
  starts on it too.
- Action screens: `matter-deadlines` and `limitation-assessment` both map to subject kind
  `matter`, capability `deadlines.view`. Open: limitation assessment from intake may need a
  `lead` subject kind.
- `RiverActionQueue` uses the run id as the job's event id so each request is its own job.
- Left: Postgres adapters for BillBalances, MatterTrustAccounts, PublishedPacks, MatterTime,
  SubjectMatters, ProposedTasks, ConflictCheckParties, MatterContacts, DocumentDrafts,
  EmailDrafts, NoteDrafts (none exist); the caller passes them in `AgentPorts`. Not done: the
  PlanStore step map (kernel `agenttools.Step` has no step id; needs an additive kernel field).
  `ForgetRunner` calls Provenance.Forget on run end; it wraps the action route's Runner, and the
  job-run worker must use it too once it exists.

## Q-BOOT-PORTS-B matter-side agent ports, step ids, action route

- `boot.WireMatterPorts` (matter reads go through `matter.Query.Get`, ARC-I-041) fills PublishedPacks, SubjectMatters, ProposedTasks,
  ConflictParties, MatterContacts, DocumentDrafts, EmailDrafts and NoteDrafts with Postgres
  adapters (app/legal/boot/agent_ports_matter.go), sets Plans to a Planner over PGPlans and
  makes one shared Provenance. Drafted text goes through `matter.Bodies` (DM-020); rows hold
  the reference only.
- Table mapping (no new migration): published packs = newest `cal.rule_pack` row per code
  with status `active`, rules jsonb decoded with the pack YAML rule keys; document draft =
  `docs.draft` (title in `instruction`, body ref and citations in `source_scope`); email draft
  = unsent outbound `comms.comm_item` plus a proposed `comms.comm_filing` (the run id waits in
  `participants`, since `drafted_by_agent_run_id` needs an approval id); note draft =
  unconfirmed `matter.note`; proposed task = status `proposed` of the newest `task`
  workflow; proposed party = `conflict.check_party` row, preparer in the check's
  `updated_by`. Resolved by Q-DRAFT-FIX: drafts keep their idempotency key, parties their
  source citation, and the email draft's run id has its own column.
- SubjectMatters resolves matter, document_draft, agent_note, email_draft, deadline,
  billing.bill, timekeeping.time_entry and conflict_check; any other kind fails closed (404).
- Additive kernel contract change: `agenttools.Step` gains `ID` (the report_plan step id),
  stored in the agent.plan body as `id`; `agenttools.StepIDs` (`SetStepIDs`) and
  `StepIndex` are new, and Planner implements StepIDs. report_plan stores the ids, so
  report_step finds a step after a restart; the in-process map stays as the fallback.
- Provenance: `AgentWriteDeps` puts the shared Provenance on WriteDeps. Boot wraps the action
  route's Runner and every runner listed in `AgentWiring.Runners` with `ForgetOnEnd`.
- OpenAPI: `POST /agent/actions/{feature_id}` added additively (Q-FEAT-CORE-12), listed in
  kernel/contract/additive.txt; `trigger.ActionPath` drops its `/api` prefix to match the /v1
  server base. It is in api/unserved.txt because the app conformance test mounts only
  Register routes; boot mounts it when `AgentWiring.Actions` is set.

## Q-DRAFT-FIX idempotent drafts, party citations, email draft run id, email.filed

- Idempotent drafts (additive migrations, each in its owning block): `idempotency_key text`
  on `docs.draft` (5800, app/legal/docs), `matter.note` (5100, app/legal/matter) and
  `comms.comm_item` (6001, app/legal/comms), each with a partial unique index on
  `(agent_run_id, idempotency_key)` built CONCURRENTLY in its own file (5801, 5101, 6002).
  The adapters look the key up first, so a retried call returns the stored draft without
  writing a second body; an insert that loses a race (`ON CONFLICT DO NOTHING`) returns the
  winner's id, and its body object is left unreferenced. Only an agent run's keyed call is
  deduplicated: the manual path (no run id) stores each call.
- Email draft run id: `comms.comm_item.agent_run_id uuid` (6001, nullable) holds the drafting
  run; `participants` no longer carries `run_id`. `drafted_by_agent_run_id` stays the run of an
  approved outbound message (its CHECK needs an approval id); the gate's effect stamps it.
- Conflict party citations: `conflict.check_party.source jsonb` (5203, nullable) holds the
  proposed party's source as `{version_id, page, from_line, to_line}`, a pointer and never
  text. `PGConflictParties` writes it, validated with `agenttools.ValidCitation` (ARC-I-038).
- Additive a2 event `email.filed` (module comms, subject comm_item, payload comm_item_id,
  matter_id, filed_by), in spec/eng/a2-interfaces.json and .html. `comms.EmailFilingStore`
  `FileEmail` files an email to a matter on `comms.comm_filing` and emits it in the same
  transaction (ARC-I-030); filing an already filed email emits nothing. F-DL-01's binding
  (`boot.JobBindings`) lists `document.uploaded` and `email.filed`. Open: no HTTP route or
  inbound-mail rule calls `FileEmail` yet; the route that files an email must wire it with the
  outbox Emitter.

## Q-HTTP-10c TrustLedger declares overdrawn and shortfall_cents

- Closes the open item under HTTP-10b. Owner decision: the approved-additions list moves
  from kernel/contract/additive.txt to api/contract-additive.txt, because it lists openapi
  pointers and describes the api contract, not kernel code. kernel/contract/openapi_test.go
  reads it from the new path with the same checks (every addition listed with a
  QUESTIONS id, no stale lines, no duplicates). TestKernelHasNoLegalTerms and
  tools/kernel_allow.txt are unchanged; nothing is exempted.
- api/openapi.yaml: `TrustLedger` gains `overdrawn: {type: boolean}` and
  `shortfall_cents: {$ref: Cents}` (>= 0), as GET /trust/ledgers already sends (BIL-I-010,
  By-law 9). Optional response fields are additive under ARC-I-001; listed in
  api/contract-additive.txt as `/components/schemas/TrustLedger/properties/overdrawn` and
  `/components/schemas/TrustLedger/properties/shortfall_cents`.
- The trust ledger list tests validate the whole page against the GET /trust/ledgers 200
  schema and fail on any ledger field TrustLedger does not declare.

## HTTP-14b: store processor_fee_cents on the payment

- `payment.Capture` gains `ProcessorFeeCents` (additive). POST /payments passes it through
  and the Payment body returns it on create and get. A fee below 0 or above amount_cents
  answers 400 validation_failed; paymentfake refuses the same with ErrInvalid.
- No migration: `billing.payment.processor_fee_cents` already exists in the frozen
  0001_init.sql (`bigint NOT NULL DEFAULT 0`, `CHECK (deposit_to = 'operating' OR
  processor_fee_cents = 0)`), so an additive nullable column would duplicate it. The
  Postgres payment service, when built, writes Capture.ProcessorFeeCents to that column.

## Q-HTTP-16 Direct trust receipt posting versus the gate-owned receipt key

- HTTP-16 accept: an unambiguous, under-limit POST /trust/receipts answers 201 and posts the
  receipt (Ledgers.Receive) in the route; openapi says the same ("Posts directly when
  classification is unambiguous and cash is under limit").
- app/legal/gateowned.go lists `trust_txn.posted=receipt` as owned by
  trust.receipt_classification (ARC-I-012). The route must declare what it writes, so the
  201 path would declare that key and TestRoutesConformToOpenAPI's gate-owned-state guard
  fails. Declaring a different key for the same write would hide the conflict.
- Options: (A) split the key: `trust_txn.posted=receipt_classified` stays gate-owned and the
  route declares `trust_txn.posted=receipt_unambiguous` as route-owned, with the
  unambiguous test written down (ledger found for account+matter, no payment_id, method not
  cash or cash under the limit); (B) every receipt goes through trust.receipt_classification
  (always 202), which contradicts the openapi summary and the HTTP-16 accept line.
  Proposed: A. HTTP-16 is not started until this is answered.

## HTTP-17: trust withdrawal request and cancel routes

- POST /trust/withdrawals returns 202 with the openapi WithdrawalRequest body (status
  awaiting_authorization, gate_id), not the Gate body the accept line names: openapi wins.
- The route records the request through withdraw.Service.Request and opens the gate itself
  (trust.withdrawal, or billing.trust_fee_withdrawal when bill_id is set). A real Service
  must not also open a gate; please confirm.
- New ports in http_request.go (Service stays frozen): LedgerReader (ledger_id -> account and
  matter), Cancels (row version, ledger, bill; records the cancellation), Gates (Open, Withdraw).
- Method wire answers 422 (no wire method in the module); direction_document_id carries the
  Form 9A EFT authorization id. Only the preparer can withdraw the gate (403 otherwise).

## HTTP-18: trust reconciliation import, prepare and submit routes

- New ports in http_write.go (Service stays frozen): Parser (CSV/OFX file -> Statement,
  ErrUnparseable), Versions (row version for If-Match), Gates (Open, Get). No parser exists on
  main yet; boot wiring must supply one.
- Import answers 202 `{statement_id, lines}` (openapi has no body schema) and posts no journal
  entry. A statement whose lines do not sum to its closing balance answers 422
  unparseable_statement.
- Prepare: the imported statement's closing balance is the bank balance (answered, HTTP-18b).
  A bank_balance_cents that differs from it answers 422 `bank_balance_mismatch` before
  anything is prepared; it is never ignored. The check reads the closing balance through a new
  `Statements` port (Closing), since Service is frozen; reconfake implements it and boot
  wiring must supply the Postgres one (until then the routes answer 503).
- Submit is refused 422 unexplained_difference unless the three-way totals agree after
  reconciling items; a signed-off reconciliation answers 409 period_locked.
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008, kernel/idem).

## HTTP-19: trust request routes (approved scope)

- No trust-request service existed; the owner approved a minimal one because the operations
  are in the frozen openapi (same rule as HTTP-05). `trust/request.go` adds `Request`,
  `Requests` (Create, Get, List) and the Postgres `RequestStore` on a new table
  `trust.trust_request` (migration `5504_trust_request.sql`, trust block). `trustfake.Requests`
  is the in-memory fake. Boot wiring supplies the store; until then the routes answer 503.
- Create writes a draft only. The agent may draft for the person it acts for
  (`prepared_by_agent`), but never approves; the preparer never approves their own request.
  Both are enforced by `CheckApprover` and the table's CHECK (approved_by <> prepared_by).
  There is no approve route yet: approval belongs to a later gate step.
- GET needs trust.view, POST needs trust.request; walls and RLS limit rows. openapi has no
  body or response schema for these operations; the views are documented here until it does.
- `billing.retainer_request` (0001) is left untouched; reconciling the two is a follow-up.
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008), as in HTTP-05.

## HTTP-20: document list, upload and page text routes

- openapi gives GET/POST /documents no body schema. POST /documents takes JSON
  `{matter_id, folder_id, kind, filename?, content}` with `content` base64 (32 MiB cap); it
  answers 201 `{id, matter_id, kind, folder_id, version_id}`. The page route answers
  `{document_id, version_id, page, lines:[{no, text}]}`.
- The routes read and write through a `DocumentStore` port in http.go (List, Document,
  Upload, HasVersion) plus kernel/docs `Store.Lines`; boot implements Upload as
  kernel/docs `Store.Put` and `Folders.File` in one tx. No contract.go change.
- A version that belongs to another document answers 404, as does a page out of range
  (kernel/docs ErrBadRange). An unknown kind answers 422 `docs.unknown_kind` before any
  service call. Upload checks access on the matter subject (`matter.matter`), so a walled
  matter answers 404.
- Routes register from http.go's init through `add`, so register.go is unchanged.
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008, kernel/idem).

## HTTP-21: document release and agent draft routes

- Built on task/HTTP-20 (same package; it reuses that branch's helpers). http.go gains one
  split: `authorize` now calls `check`, so the release routes share the principal and
  capability checks.
- POST /documents/{document_id}/release takes `{channel, version_ids}` (channel per the
  docs.outgoing payload schema; each version must belong to the document, else 422) and
  opens `docs.outgoing` with `{matter_id, document_version_ids, channel}`. It answers 202
  with the Gate body and declares `document.release_requested`; `document.released` stays
  gate-owned. The release effect itself is boot work (docs.outgoing is still in
  app/legal/boot/unwired_gates.txt).
- The route checks `documents.release` as the accept line says; the gate still refuses the
  preparer as approver.
- GET /drafts reads a `Drafts` port in http_release.go (List by DraftFilter); boot
  implements it over docs.document and the agent run log. Item shape:
  `{document_id, version_id, matter_id, kind, run_id, acting_for, created_at}`.
- The Gate body copies bill's gate rendering (min_approvers 1 per spec/gates.json); a
  shared renderer in kernel/gate would be a follow-up.

## HTTP-22: deadline list, propose and get routes (kernel/deadline)

- `deadline.Engine` has no List: the route reads a `Lister` port in kernel/deadline/http.go
  (List by ListFilter, ordered by id). PGBook implementing it is a boot follow-up. No
  contract.go change.
- The kernel handler takes every app name from `deadline.HTTPConfig` (capabilities, the
  parent subject kind and its body/payload key, the deadline subject kind, the confirm
  gate). app/legal/register.go supplies `DeadlineRoutes` (deadlines.view, deadlines.edit,
  matter.matter, matter_id, deadline.deadline, cal.confirm) and mounts it from Register
  after the modules loop: modules.go is one entry per package with a register.go, and
  kernel/deadline is not an app package.
- POST /deadlines takes `{matter_id, title, pack_id, rule_id, trigger_date}`; it computes,
  saves the deadline proposed and opens cal.confirm with the gates.json payload
  (`source` = pack id and version) in one tx, then answers 202 with the Gate body. A rule
  that yields more than one deadline answers 422. It declares `deadline.status=proposed`;
  Engine.Confirm is only the gate effect's (still listed in boot/unwired_gates.txt).
- ErrUnknownRule and ErrUnknownPack answer 422 `unknown_rule` (proposed code).
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008, kernel/idem).

## HTTP-23: work-item routes (approved scope; files outside the task)

- The routes serve over the existing kernel `work.Service` port (Create, Get, List, Move,
  SetApprover) and its `workfake`. There is no Postgres work.Service yet: boot must wire one
  (PORT follow-up); until then the routes answer 503.
- Additive contract change: `kernel/work.Filter.After` (page cursor, id order), implemented in
  workfake, so GET /work-items can page with the opaque cursor.
- New package `app/legal/work`: one line in `app/legal/modules.go` and the module count in
  `app/legal/register_test.go` (36 -> 37).
- PATCH changes `status_id` (a workflow move) and/or `approver_id`; other fields are not
  editable until the service has an update method. If-Match is checked against the item's
  version; Move runs before SetApprover (which bumps the version). The approver is never the
  assignee or an agent (422 self_approval); a missing transition is 409 transition_not_allowed.
- A matter-scoped item uses subject kind "matter" (as the 6100 backfill and agent tools do);
  wall checks use the matter registry subject (matter.matter) and answer 404.
- openapi has no body or response schema for these operations; the views are documented here.

- Open: a fee transfer to the firm's own general account (spec trust.json "Payee is firm
  general account (known)") is not a matter contact, so prepare_trust_withdrawal now refuses
  it. Boot must decide whether `MatterContacts` includes the firm's general account for fee
  withdrawals, or whether a separate firm-payee id is allowed.

- Closed by FEAT-FIX-B: a fee transfer to the firm's own general account (spec trust.json
  "Payee is firm general account (known)") was refused because it is not a matter contact.
  Decision: a separate firm-payee id. See the FEAT-FIX-B entry below.
- draft_bill reads a new `agent.UnbilledWIP` port (matter's unbilled entry ids up to
  period_to) from Deps; boot (BOOT-FEAT-WIRE) must wire it. Every line action source is
  checked against it before `batch.Drafts.Create`, so a bad source creates no draft.

## FEAT-FIX-B firm general account as a fee payee (By-Law 9 s.9(1))

- Decision: `WriteDeps.FirmAccount` (the firm general account's payee id) and `WriteDeps.Bills`
  (`agent.FeeBills`, returns `withdraw.FeeBill` for a bill on the matter) are new required
  ports; nil fails closed (ErrMissingDep). No BillBalances port existed, so FeeBills is the
  equivalent.
- The firm account is a payee only when purpose_category is fees, bill_id is a bill on the
  same matter, the bill is delivered and amount_cents <= its balance due (rules BIL-L-037,
  BIL-L-035). Otherwise `ErrFirmPayeeNeedsBill` (422), `withdraw.ErrBillNotDelivered` or
  `withdraw.ErrExceedsBillBalance`. Every other payee must still be a matter contact id.
- `withdraw.Request` has no bill field, so the bill id is checked by the agent but not carried
  into the trust request. Boot (BOOT-FEAT-WIRE) must wire FirmAccount and Bills; the trust
  module's own ValidateFee remains the authoritative check at authorization.

## WO-FIX: write-off draft outcomes and the split after a trust fee transfer

- Additive in `app/legal/billing/credit/contract.go`: `StatusRejected` and `StatusWithdrawn`.
  `PGWriteOffs.Close(ctx, tx, id, status)` moves a submitted draft to one of them; both are
  final. Migration 5620 widens the `billing.write_off_draft` status check (NOT VALID,
  validated in 5621) and replaces the guard so a row that has left `draft` never changes.
  Only `status = 'draft'` drafts count toward the second-approval total (BIL-C-085).
  Open (BOOT): call `Close(..., StatusRejected)` from the G-B5 gate rejection outcome and
  `Close(..., StatusWithdrawn)` from a preparer withdraw route.
- Q-INTEG-12.1 applied: on a bill with trust fee transfers (authorized, executed or applied,
  net of excess) the draft split uses `DraftOpenBuckets`: the bucket outstanding less the
  transfer must equal the balance, and the balance less open interest is apportioned over
  fees, disbursements and tax in the bill's issued proportions (BIL-C-205, ETA s.231). The
  transfer itself is never re-split. An apportioned bucket above its outstanding fails closed.

## Q-FEAT-CORE-15 Read and trust withdrawal handlers join the explicit handler set

## Q-FEAT-DL-08 Deadline feature tests: stand-ins and files beyond the task list

- Task: FEAT-DL-08 (F-DL-07 Post-event follow-up)
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (F-DL-07 in
  appDefs, as FEAT-CORE-17 asks) and the generated skill
  `paploo/counsel-plugin/skills/f-dl-07-post-event-follow-up/SKILL.md`.
- The agenttest Seed's Deps carry no deadline engine, published packs, subject lookup or
  proposed-task status, so every `agent.Handlers()` tool those need ran as a stand-in. The
  deadline feature tests build them in a test helper (`toolEnv` in
  `ai_f_dl_07_feature_test.go`, reused by F-DL-08 and F-DL-09) and wrap each read tool in
  the run's Provenance. Proposed: move these ports into `agenttest.Seed.Deps`.
- `get_matter` and `read_document` have no `Handler` (only the older `ReadTools`), so they
  still run as stand-ins and the tests assert exactly those two. Proposed: a `Handler` for
  each. `agenttest.QuotedOnly` then also needs to read the real `read_document` output,
  whose quoted text sits under `pages[].lines[]`, not in a top-level `agenttools.Quoted`.
- Defs set `Grant` to the capabilities of their tools: an empty grant refuses every call.
- The event type `calendar.event_ended` is named here; no calendar module publishes it yet.

## FEAT-DL-11 F-DL-05 daily digest (also FEAT-DL-12, FEAT-DL-13)

- appDefs (FEAT-CORE-17) lives in `app/legal/agent/feature`, and every feature Def imports
  that package, so `feature.appDefs()` cannot list `deadlines.DigestDef()` without an import
  cycle. The Defs are not in appDefs yet; the list has to move to a package that imports the
  modules (app/legal/boot) or take the Defs as a parameter. Decision needed before the skill
  export covers deadline features.
- agenttest `Seed.Deadline` files deadlines on subject kind `"matter"`, but list_deadlines
  (FEAT-TOOL-05) keeps only `roles.SubjectMatter` (`"matter.matter"`), so a seeded deadline
  never lists. The F-DL feature tests save their deadline on `roles.SubjectMatter` directly;
  Seed.Deadline should use `roles.SubjectMatter`.
- get_matter, read_document and compute_deadline are not in `agent.Handlers()`. The tests run
  list_deadlines as its real handler and read_document as a quoting handler over the seed's
  document text (so CheckAnswer has real citations); get_matter and compute_deadline run as
  stand-ins.

## Q-FEAT-DL-04 F-DL-02 cannot reach its tool-gap kind cal.second_check

- spec/ai/features.json gives F-DL-02 the tool_gap kind `cal.second_check` (G7b), but its tool
  list has no drafting tool: `propose_deadline` opens cal.confirm and records no draft, and
  `submit_for_approval` only submits a draft this run created (404 otherwise). So the production
  Def can never open G7b. `TestFDL02_FromFeature` pins that (second check without a draft ->
  ErrNotFound); `TestFDL02_SecondCheckViaDraft` adds `draft_document` and `documents.edit` to the
  Def and shows draft_document -> submit_for_approval opens G7b pending with the acting user as
  preparer.
- Proposal: add `draft_document` to F-DL-02's tools in spec/ai/features.json and to
  `LimitationAssessmentDef` (Tools and Grant `documents.edit`), then regenerate the skill.
- Shared: `get_matter`, `read_document` and `search_matters` have no `agent.Handler`
  (agent.ReadTools only), and `agenttest.Seed.Deps` carries no deadline engine, PublishedPacks,
  Provenance, DocumentLookup or SubjectMatters, and does not wrap read tools in
  `Provenance.ReadTool`. The deadline feature tests add these in-test (dlWorld). `agenttest.QuotedOnly`
  only knows the `agenttools.Quoted` envelope, while read_document returns
  `{"untrusted": true, pages}`; the tests check that envelope instead (docQuotedOnly).
- Event names: F-DL-01 binds `document.uploaded`; F-DL-02 binds `lead.created` (intake) and
  `matter.updated` (claim facts), all a2 events.

## Q-FEAT-DL-09 propose_deadline opens only cal.confirm, so F-DL-08 cannot open cal.change

- Task: FEAT-DL-09 (F-DL-08 Adjournment watcher)
- F-DL-08 declares gate kind `cal.change` (spec/ai/features.json). `propose_deadline` saves the
  deadline and opens `cal.confirm` itself, after `CheckGateKind`; `cal.confirm` is outside the
  F-DL-08 run scope, so a correct run is refused with `ErrGateKindNotAllowed` before anything
  is saved, and `submit_for_approval` has no draft to submit (a propose result is a gate, not
  a draft). The run fails closed; no `cal.change` gate can open, so the pending-gate, 403 and
  ErrSelfApproval checks cannot run for F-DL-08 yet. `TestFDL08_FromFeature` pins this.
- Proposed: `propose_deadline` opens the deadline gate kind the run scope allows
  (`cal.change` when the feature declares it, else `cal.confirm`), or returns a draft
  proposal that `submit_for_approval` opens under the run's kind. Either changes
  `app/legal/agent/tools_propose_deadline.go` (FEAT-TOOL-15), not this task's files.
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (F-DL-08 in
  appDefs). F-DL-08 is an extraction feature, so it gets no plugin skill.
- Event types `calendar.event_moved` and `calendar.event_cancelled` are named here; no calendar
  module publishes them yet.

- Resolved by TOOL-FIX-C: `propose_deadline` with a `deadline_id` opens `cal.change` on that
  stored deadline and holds the new date in the gate payload without applying it; without one
  it saves a new deadline under `cal.confirm`. The run scope's gate-kind check still applies.
  `TestFDL08_FromFeature` now checks the pending gate, the 403 for the agent, `ErrSelfApproval`
  for the acting user and another lawyer's approval; a Def without `cal.change` is refused.

## Q-FEAT-DL-10 create_task returns a proposal, so F-DL-09 cannot open work.absence

- Task: FEAT-DL-10 (F-DL-09 Absence coverage check)
- `create_task` creates the cover task in the proposed status and returns
  `OutcomeProposal`; only an `OutcomeDraft` result is recorded as a draft of the run, so
  `submit_for_approval` refuses `work.absence` on the cover proposal with `ErrNotFound`, and
  no write tool in the Def opens `work.absence` itself. The run fails closed (the task stays
  proposed, no gate), and the pending-gate, 403 and ErrSelfApproval checks cannot run for
  F-DL-09 yet. `TestFDL09_FromFeature` pins this and checks a kind outside the Def is
  `ErrGateKindNotAllowed`.
- Proposed: `create_task` records its proposed task as a draft of the run when the run scope
  allows `work.absence` (or returns `OutcomeDraft`), so `submit_for_approval` can open the
  gate. That changes `app/legal/agent/tools_task.go` (FEAT-TOOL-16), not this task's files.
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (F-DL-09 in
  appDefs) and the generated skill
  `paploo/counsel-plugin/skills/f-dl-09-absence-coverage-check/SKILL.md`.
- Event type `work.absence_approved` is named here; work/team-availability does not publish
  it yet.

## Q-FEAT-CON-01 F-CON-01 party extraction

- Files: the task lists `app/legal/conflicts/ai_f_con_01_test.go`; the landed AI-033 test is
  `app/legal/conflict/ai_f_con_01_test.go` (package `conflict`), so that file was moved to
  FromFeature in place. `app/legal/agent/featureset/featureset.go` gained
  `conflict.PartyExtractionDef()` (FEAT-CORE-17 registration); F-CON-01 is an extraction Def,
  so no plugin skill is generated for it.
- G2 has no kernel gate here: propose_conflict_parties runs the conflict check, and the open
  check run is the pending conflict.clearance (G2), decided through `conflict.Clearance`
  (gates.CheckDecision). So the tests assert the run is open with the acting user as preparer,
  an agent decider is 403 (`gates.ErrAgentApproves`) and the acting user is refused as preparer
  (`gates.ErrPreparerApproves`, the self-approval refusal of app/legal/gates), not
  `agent.ErrSelfApproval` (stand-in gates only).
- `get_matter`/`search_matters`/`read_document` have no `agent.Handler` and `agenttest.Seed.Deps`
  carries no `ConflictCheckParties`, `conflict.Checker`, `DocumentLookup` or `SubjectMatters`; the
  test adds them (con01World). `agenttest.QuotedOnly` does not know read_document's
  `{"untrusted": true, pages}` envelope; the tests check it (docQuotedOnly).
- Events: `lead.created` (intake submitted) and `document.uploaded` (document added), a2 events.
- The landed test retried on `ai.ErrTimeout`; FromFeature's default RetryOn does not, so the
  test sets it as the landed test did.

## Q-FEAT-CON-02 F-CON-02 opens G2 through propose_conflict_parties

## WD-BILL: trust withdrawal names its bill

- As in Q-FEAT-CON-02, the G2 gate is the conflict run propose_conflict_parties starts; no
  F-CON tool drafts, so the memo is the run's final answer, not a stored draft. A memo draft
  store with a draft tool would let submit_for_approval carry it.
- a2 lists no event for "all hits disposed"; F-CON-03 binds `conflict.hits_disposed`, which
  the conflict module does not publish yet.
- The AI-035 test (app/legal/conflict/ai_f_con_03_test.go; the task's
  app/legal/conflicts/ path is a typo) now runs through agenttest.FromFeature with no
  stand-ins. The f-con-03 fixtures use a2 inputs, the injection variant no longer makes an
  extra tool call, and ok pins match.prompt_hash.
- search_matters and get_matter return matter short names unquoted (not as
  agenttools.Quoted), so an instruction in a matter name reaches the model outside quoted
  data (ARC-I-038). That is a handler follow-up.

## PORT-WORK: Postgres kernel work.Service

- `work.PG` (kernel/work/pg.go) implements `work.Service` over work.work_item, work.status and
  work.transition. Boot must wire it (with the app's `SubjectKinds`) for the HTTP-23 routes.
- Shared contract tests live in `kernel/work/worktest`; workfake runs them in unit tests and
  work.PG runs them in `pg_integration_test.go` (vet-only for now).
- Carries the HTTP-23 additive `work.Filter.After` cursor (same hunk as task/HTTP-23, so the
  merge is clean). PG pages in id order (uuidv7, creation order).
- Keys: an empty Item.Key becomes `W-` plus the random 12-hex tail of the uuidv7 id; rank
  starts as the id. A readable per-firm series (kernel/numseq) is a later choice.
- Merges task/TOOL16-DESC: work.PG writes and reads `Item.Description` (work_item.description,
  NULL for none; the 1003 check bounds it).
- Automations (work.Automations) have no Postgres implementation yet.

## TOOL16-DESC: work item description

- Additive in `kernel/work/contract.go`: `Item.Description *string` (Markdown, nil for none),
  answering the FEAT-TOOL-16 note above. `PrepareItem` refuses one longer than
  `MaxDescriptionLen` (10000, the create_task schema maximum) and turns a blank one into nil.
  `create_task` now stores `description_markdown` on the proposed item.
- `work.work_item.description` already exists as a nullable column in 0001_init.sql, so the
  migration (1003, validated in 1004) adds only the matching length/non-blank check, NOT VALID.
  There is no Postgres `work.Service` yet; when one is written it reads and writes the column.

## PORT-16b: written-off interest to OP-1110

- Additive in `app/legal/billing/credit/contract.go`: `WriteOff.InterestCents`, read by
  `PGWriteOffs.Lock` from the draft's `interest_cents` bucket. `BadDebtEntry` now follows
  BIL-J-009 in full: Cr OP-1100 the amount less interest, Cr OP-1110 the interest; Dr OP-6100
  still takes the pre-tax amount plus interest. Tax plus interest above the amount fails.
- Open: `ApproveWriteOff` computes the tax adjustment on the whole amount, interest included
  (`BadDebtTax(amount, billTax, billTotal)`). Interest carries no HST, so the relief should
  probably be on the amount less interest, or simply the draft's tax bucket. Not changed here.

## PORT-STMT: Postgres recon Statements.Closing

- `recon.PGStatements` (statements_pg.go) implements the HTTP-18b `Statements` port: the
  closing balance, in cents, of the latest trust.bank_import whose period covers the month's
  last day. No statement (or only imports without a stored balance) is `ErrNotFound`.
- trust.bank_import had no balance columns, so `5530_bank_import_balances.sql` (app/legal/trust
  block) adds nullable `opening_cents` and `closing_cents`. The Postgres ImportStatement (boot
  follow-up; none on main) must write both.
- Built on main without HTTP-18b, so the interface check is in the test; after HTTP-18b merges,
  add `var _ Statements = (*PGStatements)(nil)` and wire it as `WriteHTTP.Statements` at boot.

## Q-HTTP-15 Gated payment routes: proposed allocations and refund gate

- POST /payments/{payment_id}/allocations: the sum must equal the payment (422
  allocation_sum_mismatch, ARC-I-029). A set of operating bills only, each named once, is
  proposed directly (200, allocation state `proposed`) through a new `payment.Proposals`
  port; the route declares `payment.allocation=proposed` and never
  `payment.allocation=applied`. Unapplied money (client credit) or a trust_ledger row opens
  billing.payment_allocation (202). The payment version is 1, as GET reports it.
- POST /payments/{payment_id}/refunds opens billing.refund (202) with payee = the payment's
  payer of record and reason = reason_code; an amount above the payment answers 409
  refund_exceeds_payment. The billing.refund effect (provider call keyed <gate_id>:<version>)
  and its registration are not built yet: an approved refund pays nothing until they are.
- Boot must wire `payment.Gated` (DB, Gates, Proposals and the HTTP dependencies); until it
  does, both routes answer 503.

## HTTP-25: agent runs, start and get (approved scope)

- agent.Service has no start or run query, so http.go adds the narrow `RunService` port
  (SkillGrant, Start, Run) with `ErrUnknownSkill` (422 unknown_skill) and `ErrRunNotFound`.
  Boot must wire the Paploo starter and a run store over agent.agent_run; until then the
  routes answer 503.
- POST /agent/runs takes `{skill_id, inputs?: object, matter_id?}`. The route mints the run id,
  asks `Access.AgentPrincipal(run, caller, skill grant)` (ARC-I-036 intersection) and hands
  that principal to Start; it answers 202 with the run view. A walled matter answers 404.
- GET /agent/runs/{run_id} answers 404 for a run another member started or one on a walled
  matter. Neither op lists, so the cursor and limit accept lines do not apply.
- A reused Idempotency-Key with a different body answers 422 (kernel/idem, ARC-I-008).

## HTTP-24: admin users, role change, roles, audit events (files outside the task)

- New package `app/legal/access` routes: one line in `app/legal/modules.go` and the module
  count in `app/legal/register_test.go` (36 -> 37; 38 once HTTP-23's work module also merges).
- Narrow read ports in http.go (the service gaps): `Directory` (Users, User, CheckRoleChange),
  `RoleCatalog` (Roles) and `AuditQuery` (Events, after a decimal sequence). Boot must wire
  Postgres ones; until then the routes answer 503. Audit detail leaves through `audit.Masker`.
- PUT /users/{user_id}/roles takes `{"role_id": uuid}` (core.app_user holds one role) and opens
  core.role_change with payload `{role_id, change: {user_id, from_role_id, user_version}}`
  (the kinds.json schema). It assigns nothing; the gate effect does. If-Match is the user's
  row version. `last_admin` (409) and `unknown_role` (422) come from CheckRoleChange.
- A reused Idempotency-Key with a different body answers 422 (kernel/idem, ARC-I-008), not
  the 409 the accept line names; same as the other HTTP routes.
- openapi has no body schemas for these operations; the views are documented here.

## EVENTS-2: publishers for the remaining F-DL-03 / F-DL-04 events

- Branch carries origin/task/DL-EVENTS (merged in) because it reuses `PGBook.Events` and
  `FilingStore.Events`; merge DL-EVENTS first or together.
- New minimal write points (no schema change), each emitting in the writer's tx and refusing
  before any write when its emitter is nil (DL-EVENTS pattern):
  - `deadline.trigger_date_changed`: kernel/deadline `PGBook.ChangeTrigger(ctx, tx, id, date)`
    updates `cal.deadline.trigger_on` only (due date stays until F-DL-03's proposal is
    confirmed), subject `cal.deadline`/id, body ids plus old and new trigger. Same date is a
    no-op. Not on the frozen `Engine` interface.
  - `claim.issued`: comms `FilingStore.IssueClaim(ctx, filingID, courtFileNo, issuedOn)` sets
    the court file number on a filed `legal.court_filing` row (ErrNotFound otherwise).
  - `service.affidavit_received`: comms `FilingStore.RecordAffidavitOfService(ctx, f)` records
    a filed filing whose document is the affidavit; it emits this event instead of
    `filing.receipt_received`, so F-DL-04 runs once per record.
- Open: no handler or route calls these yet (boot/handler task). Whether an affidavit of
  service received but not filed with the court should also publish (verify).

## Q-BOOT-WIRE-A agent layer at boot

- `Options.Agent` (`AgentWiring`) registers every `agent.Handlers()` tool, the job bindings
  and tick job, the action route, the flag digest template and the FlagSink gate map. Boot
  fails closed: a nil port fails its handler with ErrMissingDep, and `Features()` fails when a
  feature names a tool with no handler or a gate kind outside spec/gates.json.
- Event names chosen from a2 for unbound features: F-DL-01 `document.uploaded`; F-DL-02
  `lead.created` and `matter.updated` (claim facts updated). Checked (BOOT-PORTS-B) against
  spec/ai/features.json triggers ("document filed to matter"; "intake created or claim facts
  updated") and the a2 event catalogue: all three are the a2 events for those triggers and
  stay. a2 had no comms filing event; Q-DRAFT-FIX adds `email.filed`, and F-DL-01 now
  starts on it too.
- Action screens: `matter-deadlines` and `limitation-assessment` both map to subject kind
  `matter`, capability `deadlines.view`. Open: limitation assessment from intake may need a
  `lead` subject kind.
- `RiverActionQueue` uses the run id as the job's event id so each request is its own job.
- Left: Postgres adapters for BillBalances, MatterTrustAccounts, PublishedPacks, MatterTime,
  SubjectMatters, ProposedTasks, ConflictCheckParties, MatterContacts, DocumentDrafts,
  EmailDrafts, NoteDrafts (none exist); the caller passes them in `AgentPorts`. Not done: the
  PlanStore step map (kernel `agenttools.Step` has no step id; needs an additive kernel field).
  `ForgetRunner` calls Provenance.Forget on run end; it wraps the action route's Runner, and the
  job-run worker must use it too once it exists.

## Q-INTEG-15A-SCHEDULED Agent wiring after the pass 14 FEAT merges

- BOOT-WIRE-A was cut before pass 14 landed F-CON-01..03 and F-DL-05, 07..13. Integration
  pass 15a binds every new job feature through its own package Binding func and drops
  BOOT-WIRE-A's local copies of the deadline event names (deadlines already exports them).
- Scheduled runs F-DL-05 (digest) and F-DL-09 (daily coverage) act for
  AgentWiring.RuleAdmin. F-DL-06 acts for its designated lawyer (JOB-ACTOR,
  trigger.BindDesignated). Open: move F-DL-05 and F-DL-09 to BindDesignated too, and have
  boot call Bindings.WithDesignated with agent's Postgres designated-lawyer reader; boot does
  not do that yet, so the rule watch stays unready until it does.
- AgentPorts gains UnbilledWIP, Matters (matter.Query), DocLookup and WriteTools, the
  handler deps pass 14 added (draft_bill, get_matter/search_matters, read_document,
  prepare_trust_withdrawal).

## Q-INTEG-15A-SCREENS Action screens for the conflict and calendar features

- The action route's screen map adds conflict-check-new, conflict-results and
  conflict-clearance as matter subjects needing conflicts.run, and event-detail,
  firm-calendar and rule-trigger-wizard as matter subjects needing deadlines.view. Open: a
  conflict check before matter opening has no matter yet; confirm whether those screens
  should select a conflict_check subject kind instead.
- After EVENTS-WIRE, conflict-results and conflict-clearance select a conflict.run subject.
- After merging pass 15b (F-CON-04..17, F-DOC-01..03, 06): consents-waivers selects a
  conflict.run (conflicts.run); identity-portal, organization-identification,
  party-records-hygiene (contacts.edit) and lateral-hire-intake select a party.contact;
  client-identification (conflicts.identify), ethical-walls (conflicts.walls),
  engagement-letter-composer and ai-draft-workspace (documents.edit) select a matter.
  Open, lawyer/product to confirm: conflict-search-settings and compliance-audit-log are
  firm-level screens with no natural record; they are mapped to matter with conflicts.run
  and admin.audit_log for now.
- New job features bind through their package Binding funcs; scheduled ones without a
  designated lawyer (F-CON-05, F-CON-09..12, F-DOC-06) act for RuleAdmin. F-DOC-06 binds
  to rulepack.published for "a known rule change"; no form-change event exists yet.
  AgentPorts gains DeadlineGetter (propose_deadline).

## BOOT-PORTS-A billing and trust agent ports

- The base (BOOT-WIRE-A) has no `agent.UnbilledWIP`, `WriteDeps.FirmAccount` or `WriteDeps.Bills`;
  they come from FEAT-FIX-A/B. This branch merges origin/task/FEAT-FIX-B (conflicts in
  tools_draft_bill_test.go, renamed to the draftBill* names, and in this file).
- Outside the new boot file: `AgentPorts.UnbilledWIP` (agent.go, one field and one ports() row)
  and its test stub, so draft_bill builds. New Postgres readers: `bill.PGBalances`
  (BalanceCents, FeeBill over billing.bill_balance), `trust.PGMatterAccounts` (open accounts
  with a client ledger for the matter), `timekeeping.PGWIP` (MatterTime from the stored
  rate/amount; UnbilledEntryIDs = approved, billable, no bill, work_date <= period end).
- Fail closed: a void bill owes 0 and reads as undelivered; FeeBill balance due is net of
  reserved fee transfers and floored at 0 (no fee on an overpaid bill); a bill on another
  matter is ErrNotFound. The firm general account payee id is config
  (`BillingAgentSources.FirmAccount`); empty fails boot. No table holds it yet (verify).
- Boot calls `WireBillingAgentPorts(NewBillingAgentSources(pool, firmPayee), &ports, &writeDeps)`.
  WriteDeps.TrustAccount and Contacts are left to BOOT-PORTS-B.

## PG-BILL

- `billing.bill_guard` (0001_init.sql) freezes `signed_by`, `signed_at` and `signature_hash` once a bill
  leaves draft, but `bill.Service.Sign` signs an approved bill. `bill.PGService.Sign` issues the update
  and the trigger refuses it, so on Postgres a bill cannot be signed and therefore not delivered. Needs
  an additive guard change (allow NULL -> value once on an approved, undelivered bill) or signing before
  approval. Not changed here.
- `bill.PGService.Update` refuses a `Change.Lines` on a draft (`ErrInvalid`): bill lines are never
  deleted, so lines change only through `draft.PGDrafts.Edit` line actions.
- `draft.PGDrafts` taxes every draft at one component (boot: HST-ON at 130000 ppm). Place of supply
  per payer (BIL-C-201/207) is not applied at draft time.

## Q-PG-BILL-FIX

- `billing.bill_guard` is replaced additively by `migrations/5622_bill_sign_after_approval.sql`
  (billing block, `CREATE OR REPLACE FUNCTION`, trigger unchanged). The only change: on an
  approved, undelivered, unsigned bill, `signed_by`, `signed_at` and `signature_hash` may go
  from NULL to a value once, all three together (BIL-L-001). Every other column 0001 froze,
  `signature_ref` included, stays frozen; a partial, repeated or cleared signature is DM-041.
  `migrations/bill_guard_test.go` pins the column sets against 0001 and
  `app/legal/billing/bill/sign_integration_test.go` (integration, vet-only in `make check`)
  signs through `PGService.Sign` and refuses every other column before and after signing.
- `draft.PGDrafts` no longer has a fixed HST-ON 13%: it takes `Places` (supply facts from the
  matter's client), `Decider` (tax.PlaceOfSupply, T-014.2 CRA rules) and `Calc` and fails closed
  with `draft.ErrPlaceUnknown` when the place cannot be decided. The draft stores
  `place_of_supply_province` and `place_of_supply` ({jurisdiction, rule_applied}); approval still
  needs the lawyer's confirmation (BIL-C-201).
- Open: boot's `ClientPlaces` reads `addresses->0->>'province'` and `'country'` of the matter's
  client; the address JSON keys are not specified anywhere, so the party module should own a
  typed reader. Court and property provinces and the firm's provincial registrations are not
  recorded yet, so they are passed empty (GST only outside HST provinces). A zero-rated
  (non-resident) draft stores a NULL province, which the approval CHECK refuses until the
  zero-rated place has a representation.

## Q-TAX-FIELDS

Done in TAX-FIELDS (decision Q-PG-BILL-FIX):
- `matter.matter` gains nullable `court_province` and `property_province` (`migrations/5102_matter_tax_provinces.sql`).
  `matter.Matter` gains `CourtProvince` and `PropertyProvince` (additive contract fields), returned by `matter.Query`.
  No write path sets them yet; matter create/edit and the API do not expose them.
- `core.firm` gains `tax_registration_provinces char(2)[] NOT NULL DEFAULT '{}'`
  (`migrations/5623_firm_tax_registrations.sql`). `core` has no migration block, so the file sits in the billing
  block, billing being the only reader. Open: should `core` (or a firm-settings module) get its own block?
- boot `ClientPlaces` reads the client address typed `billing` (else the first), the matter's provinces and the
  firm's registrations. The CRA "address obtained in the ordinary course" rule is a (verify) note in spec/legal billing.
- A zero-rated (non-resident) draft stores `place_of_supply.jurisdiction = 'export'`, one ZERO tax line at rate 0,
  and `place_of_supply_province = 'EX'` (`draft.ExportProvince`): the column is char(2) and the 0001 approval CHECK
  needs it non-NULL, so approval accepts the export. Open: reports by province must treat `EX` as export, not a province.
- PG-BILL-FIX's `5620_bill_sign_after_approval.sql` collided with WO-FIX (5620/5621); renumbered to 5622.

## Q-INTEG-15A-MIGRATIONS Migration renumbering in pass 15a

- DRAFT-FIX and TAX-FIELDS both took 5100 in the app/legal/matter block. The note
  idempotency key (DRAFT-FIX, merged first) keeps 5100/5101; matter_tax_provinces moves to
  5102_matter_tax_provinces.sql. Content unchanged; both are additive.

## PG-READERS: Postgres read ports at boot

- Branch merges task/HTTP-17 and task/HTTP-18b onto task/BOOT-WIRE-B (their ports did not
  exist on the base). Their duplicate declarations with HTTP-11 (problem codes, Access,
  views, writeJSON, test helpers) are folded into one; the withdrawal view gains bill_id and
  acting_for, and both read views report version as int64.
- trust.withdrawal_request and trust.reconciliation have no row_version column. The
  Postgres Cancels.Row and recon Versions report a version ranked by status, which only moves
  forward, so If-Match still detects every status change. Proposed: an expand migration adding
  row_version with the core.touch trigger; open for the owner.
- Ledger balances: Ledgers.Balance (and NewLedgerReads) return the true signed posted and
  available figures; nothing is clamped in the reader. GET /trust/ledgers still reports per
  HTTP-10b (overdrawn + shortfall_cents beside a 0 Cents field) until openapi TrustLedger
  allows a signed balance.
- recon.NoParser is the boot default Parser: uploads answer 422 `no_statement_parser`
  ("No bank statement parser is configured") until a CSV/OFX parser lands.
- Still caller-supplied: credit.Bills (credit-note POST), withdraw.Service, recon.Service and
  recon.Statements have no Postgres implementation, so POST /credit-notes, POST
  /trust/withdrawals and the reconciliation write routes keep answering 503 until they do.

## PG-WRITES: Postgres write ports at boot

- Branch merges task/PORT-STMT onto task/PG-READERS. Boot (`withWrites`, services_writes.go)
  now fills credit.Bills (PGBills), withdraw.Service (PGService), recon.Service (PGService) and
  recon.Statements (TxStatements over PGStatements); WithdrawGates defaults to the route gates
  when they can withdraw. POST /credit-notes, POST /trust/withdrawals (+cancel) and the
  reconciliation prepare and submit routes no longer answer 503 on Postgres.
- withdraw.PGService and recon.PGService never approve: Authorize and RecordExecution refuse
  (ErrEffectOnly, ErrNotAuthorized), and recon Approve refuses (ErrEffectOnly; an agent gets
  ErrAgentApproval). Authorization stays in Effect.Apply; sign-off in PGSignOff.Sign. No
  trust.reconciliation_signoff effect is registered at boot yet; open.
- purpose_category: openapi (fees, disbursement, client_payment, third_party, refund) and the
  DDL (fees_to_operating, disbursement, client_refund, third_party, transfer_out) differ.
  PGService stores client_payment and refund as client_refund. `fees` is refused 422:
  fees_to_operating needs bill_id and withdraw.Request has no bill field. Proposed: an additive
  Request.BillID. The read side still reports the stored name; please confirm the mapping.
- POST /trust/withdrawals passes `PreparedBy: p.UserID`, which is empty for an agent, so an
  agent's request fails closed (422) instead of being prepared for its acting-for user.
  Proposed fix in http_request.go: use the acting-for user, as recon's preparerOf does.
- The route opens the gate after Request in its own transaction and does not store the gate
  id, so approval_id stays NULL until the effect authorizes and cancel does not withdraw an
  open gate. Proposed: a Cancels/Service hook to record approval_id at open.
- Request.IdempotencyKey is not stored (no column); the route's idem layer replays keys.
- Reconciliation book and client ledgers both come from trust.trust_transaction at month end
  (the trust journal); bank is the imported statement's closing balance. Proposed follow-up:
  read the book from the general-ledger trust bank control account so book vs ledgers is an
  independent check. Prepare re-prepares an open draft/ready month in place; a signed or
  imported month is ErrPeriodLocked. ImportStatement refuses with ErrNoParser.

## PG-WRITES-FIX

## Q-HTTP-16 Direct trust receipt posting versus the gate-owned receipt key

- Additive: migration `5505_withdrawal_gate.sql` adds nullable `trust.withdrawal_request.gate_id`
  (5504 is taken by another task branch). The request route stores the opened gate in the same tx;
  cancel withdraws that gate; only the preparer (the user, or the user an agent acts for) may cancel.
- Additive: `withdraw.Cancels` gains `RecordGate`, and `withdraw.Row` gains `GateID` (not contract files).
- Open: no Postgres `withdraw.FeeBills` adapter exists yet (billing owns the bill balance), so
  `Effect.Bills` is unwired at boot and every fee withdrawal fails closed at authorization.
- Open: the route opens `billing.trust_fee_withdrawal` for a bill withdrawal, but that kind has no
  effect wired (boot/unwired_gates.txt) and execution reads only `trust.withdrawal` approvals.
- Open: `PGService.Request` opens no gate, so the agent tool's `w.GateID == ""` check refuses every
  agent withdrawal on Postgres until the gate is opened in the service path.

## Q-INTEG-15A-RECON One outstanding-items function for both reconciliation views

- The read view (recon.Reads, GET) summed the reconciling items for outstanding_items_cents;
  the write view (prepare/submit responses) used adjusted_bank_cents - bank_balance_cents.
  They agree only while the stored adjusted balance matches the stored items. Both now call
  ThreeWay.OutstandingItemsCents(): the signed sum of the items (outstanding deposits
  positive, outstanding cheques negative), so By-law 9's comparison reads bank closing
  balance + outstanding deposits - outstanding cheques = book balance = sum of client
  ledgers. TestOutstandingItemsByLaw9 checks the identity and that both views agree.
- Open: should a stored row whose adjusted_bank_cents disagrees with bank + items be
  refused on read rather than reported? Today it is reported as stored.

## Q-INTEG-15A-PAGELISTER Pass 14 renamed kernel deadline.Lister to PageLister

- HTTP-22 added `deadline.Lister` (List(ctx, ListFilter)) in kernel/deadline/http.go, which
  clashed with the existing deadline Lister that agent tools read through. Pass 14
  (6c73876) renamed the HTTP one to `PageLister` with method `ListPage`, and the HTTP
  struct's `Lister` field now has type PageLister. This is a kernel Go interface change
  made at integration without a prior entry; it is recorded here. The field name `Lister`
  was kept; open: rename the field to `Pages` when the Postgres store implements ListPage.

## REFUND-EFFECT billing.refund gate effect

## Q-FEAT-CORE-15 Read and trust withdrawal handlers join the explicit handler set

- `handlerset.go` `Handlers()` lists four more handlers (search_matters, get_matter,
  read_document, prepare_trust_withdrawal) so the set covers all 25 a2 tools;
  `handlerset_test.go` lists them too. The handlers live in `handlers.go`.
- prepare_trust_withdrawal builds from the `*agent.WriteTools` boot passes in Deps, so the
  run it parks is the one that WriteTools resumes on gate.decided.
- `agenttest/seed.go`: `SeedStore.Document` implements `agent.DocumentLookup`, so
  read_document runs its real handler in feature tests instead of a stand-in.

## Q-FEAT-CON-02 F-CON-02 opens G2 through propose_conflict_parties

- No F-CON tool drafts, so the run scope never holds a draft and an agent's
  submit_for_approval is refused (404). The run's G2 gate is the conflict run that
  propose_conflict_parties starts: conflict.Clearance decides it with gate kind
  conflict.clearance, and the run's preparer is the acting user. The feature test checks that
  run is open, that an agent principal deciding is 403 (ErrAgentApproves) and that the
  acting user deciding is 403 (conflict.ErrPreparerApproves, the conflict module's
  self-approval refusal).
- The Def starts from the a2 event `conflict.check_completed` ("event: check run").
- No F-CON-02 write carries an amount or a date, so ErrUnsourcedValue (ARC-U-027) has no
  case here. propose_conflict_parties checks a party source's shape but not that the run
  read it (Scope.Cites); that is a handler follow-up.

## Q-FEAT-CON-03 Clearance memo is the run's answer; new event conflict.hits_disposed

- As in Q-FEAT-CON-02, the G2 gate is the conflict run propose_conflict_parties starts; no
  F-CON tool drafts, so the memo is the run's final answer, not a stored draft. A memo draft
  store with a draft tool would let submit_for_approval carry it.
- a2 lists no event for "all hits disposed"; F-CON-03 binds `conflict.hits_disposed`, which
  the conflict module does not publish yet.
- The AI-035 test (app/legal/conflict/ai_f_con_03_test.go; the task's
  app/legal/conflicts/ path is a typo) now runs through agenttest.FromFeature with no
  stand-ins. The f-con-03 fixtures use a2 inputs, the injection variant no longer makes an
  extra tool call, and ok pins match.prompt_hash.
- search_matters and get_matter return matter short names unquoted (not as
  agenttools.Quoted), so an instruction in a matter name reaches the model outside quoted
  data (ARC-I-038). That is a handler follow-up.

## Q-CON-03-MEMO Clearance memo kept on file; conflict.hits_disposed published

- Decided as counsel (LSO Rule 3.4 records): F-CON-03 files the memo as a draft document on
  the check's matter through draft_document (DocumentDrafts), not only as the run's answer.
  F-CON-03 gains draft_document in its Def and in spec/ai/features.json, and the grant
  documents.edit.
- The a2 draft_document input has no field that links a draft to a conflict run, so the
  memo is titled "Conflict clearance memo, run <ref>" with the ref propose_conflict_parties
  returned, and the draft carries the agent run id. A structured link (an additive subject on
  NewDraftDocument, or the run storing the memo draft id) is a follow-up. A check on a lead
  with no matter has nowhere to file the memo yet.
- conflict.Checks publishes conflict.hits_disposed (subject conflict.run) through its
  Events outbox.Emitter once, on the disposition that leaves every hit of the run disposed.
  The body is ids and a count, with acting_for set to the run's preparer. Dispositions are
  kept in memory (Q-PORT-18 C), so the event is emitted with no tx; boot must set Events, and
  while it is nil no event is published.

## BOOT-REST: services wired last

- Wired in app/legal/boot/services_rest.go behind `Options.Rest` (nil builds nothing): PGGrants
  (`matter.matter` / `matter_id`) with `AuditMasker` keeping `grant_id`; `recon.PGSignOff`
  with audit; `core.firm.sole_practitioner` recomputed at Start and every 15 min (kind
  `firm.sole_practitioner_sync`; no licensee-change event exists, and licence expiry is
  date driven); the renewal job (still behind FlagSourceSync, off); `portal.PGSessions`;
  `PGBook.Events` and `FilingStore.Events` set to the kernel emitter (DL-EVENTS); the
  trust.receipt_classification and billing.refund effects; POST /trust/receipts.
- Touched outside the new file: boot.go (Options.Rest, the hook, Start sync), effects.go
  (wiring.rest, rest effects merged in registerGates), routes.go (ReceiptHTTP mount),
  services_trust.go (`TrustPorts.FeeBills` -> withdraw `Effect.Bills`, WD-BILL; nil fails
  every fee withdrawal closed).
- Open: payment.Gated (HTTP-15 is not merged here); Postgres RefundStore, TrustRefunds and a
  withdraw.FeeBills billing adapter (taken as ports); BOOT-21 (T-031.4 reminder worker does not
  exist); WO-FIX Close on G-B5 rejection needs a kernel gate-rejection hook (registry.GateKind
  has none) and a credit preparer-withdraw route; the portal Shell is not built at boot yet, so
  PGSessions is exposed through `wiring.Wired()` only.

## HTTP-25: agent runs, start and get (approved scope)

- agent.Service has no start or run query, so http.go adds the narrow `RunService` port
  (SkillGrant, Start, Run) with `ErrUnknownSkill` (422 unknown_skill) and `ErrRunNotFound`.
  Boot must wire the Paploo starter and a run store over agent.agent_run; until then the
  routes answer 503.
- POST /agent/runs takes `{skill_id, inputs?: object, matter_id?}`. The route mints the run id,
  asks `Access.AgentPrincipal(run, caller, skill grant)` (ARC-I-036 intersection) and hands
  that principal to Start; it answers 202 with the run view. A walled matter answers 404.
- GET /agent/runs/{run_id} answers 404 for a run another member started or one on a walled
  matter. Neither op lists, so the cursor and limit accept lines do not apply.
- A reused Idempotency-Key with a different body answers 422 (kernel/idem, ARC-I-008).
- openapi has no body schemas for these operations; the views are documented here.

## HTTP-26: webhook endpoints, list, create, renew

- kernel/webhook/http.go adds the `EndpointReads` port (Endpoints, Endpoint; never the secret
  reference) because Dispatcher has no list, and `ErrEndpointExpired` (409 webhook_expired).
  The expiry write reuses `ExpiryStore` and `RenewEndpoint`. Boot must wire Postgres ones;
  until then the routes answer 503. app/legal/register.go mounts the kernel routes
  (`registerKernelRoutes`) after the modules.
- platform.webhook has no row version; `EndpointInfo.Version` is what If-Match checks, so the
  Postgres reads need an additive version column (follow-up, kernel/webhook block 14xx).
- POST opens gate platform.integration_scope with payload `{integration_id (minted endpoint
  id), scopes (events), data_leaves_canada (default true), change}`; the effect creates the
  endpoint and its secret. platform.integration_scope is still in pendingGateKinds.
- PATCH: an expiry-only change applies now (200, ETag); any change to the events (widening
  per ARC-I-049; narrowing too, since events are gate-owned) opens the gate with the whole
  patch (202). An expired endpoint answers 409 webhook_expired: create a new one.
- A reused Idempotency-Key with a different body answers 422 (kernel/idem, ARC-I-008).

## PORT-16c: bad-debt relief excludes interest

- Resolves the PORT-16b open point (decided as counsel): `ApproveWriteOff` computes the ETA
  s.231 relief as `BadDebtTax(amount - interest, billTax, billTotal)`. A write-off that is all
  interest gets no relief; interest above the amount fails with `ErrInvalidAmount`.

## Q-JOB-ACTOR Scheduled AI features act for a designated lawyer (decided as counsel)

- A scheduled job feature acts for the lawyer the firm names for it in firm settings. The
  setting is a new table `agent.designated_lawyer (feature_id, user_id)` (migration 6201,
  additive, owned by app/legal/agent), not a column on core.firm, since a module writes only
  its own tables. `agent.PGDesignatedLawyers` reads and sets it; Set accepts only an active
  human lawyer in good standing, and a read of a user who no longer is answers "".
- `trigger.BindDesignated(def, events...)` binds a scheduled feature whose runs act for its
  designated lawyer, read at each tick (`Bindings.WithDesignated`). No lawyer named, or no
  settings source, means the run does not start (fail closed, logged as a warning), and
  `Bindings.Readiness` reports the feature as `no_designated_lawyer`. `Bind(def, actingFor)`
  with a fixed acting user still works for bindings that need one.
- F-DL-06 now binds with BindDesignated (`RuleWatchBinding()` takes no argument). At merge,
  the scheduled bindings on unmerged branches (F-CON-05 ReverseSweepBinding, F-CON-10
  ExpiryRetentionBinding, F-DL-05 DigestBinding) should switch the same way; until then they
  keep compiling with a fixed acting user. Boot wiring and a readiness HTTP surface are not
  built yet: boot calls `WithDesignated(agent.NewPGDesignatedLawyers(pool))` when it builds
  the Bindings, and surfaces `Readiness` on the setup checks.

## PG-WRITES: Postgres write ports at boot

- Branch merges task/PORT-STMT onto task/PG-READERS. Boot (`withWrites`, services_writes.go)
  now fills credit.Bills (PGBills), withdraw.Service (PGService), recon.Service (PGService) and
  recon.Statements (TxStatements over PGStatements); WithdrawGates defaults to the route gates
  when they can withdraw. POST /credit-notes, POST /trust/withdrawals (+cancel) and the
  reconciliation prepare and submit routes no longer answer 503 on Postgres.
- withdraw.PGService and recon.PGService never approve: Authorize and RecordExecution refuse
  (ErrEffectOnly, ErrNotAuthorized), and recon Approve refuses (ErrEffectOnly; an agent gets
  ErrAgentApproval). Authorization stays in Effect.Apply; sign-off in PGSignOff.Sign. No
  trust.reconciliation_signoff effect is registered at boot yet; open.
- purpose_category: openapi (fees, disbursement, client_payment, third_party, refund) and the
  DDL (fees_to_operating, disbursement, client_refund, third_party, transfer_out) differ.
  PGService stores client_payment and refund as client_refund. `fees` is refused 422:
  fees_to_operating needs bill_id and withdraw.Request has no bill field. Proposed: an additive
  Request.BillID. The read side still reports the stored name; please confirm the mapping.
- POST /trust/withdrawals passes `PreparedBy: p.UserID`, which is empty for an agent, so an
  agent's request fails closed (422) instead of being prepared for its acting-for user.
  Proposed fix in http_request.go: use the acting-for user, as recon's preparerOf does.
- The route opens the gate after Request in its own transaction and does not store the gate
  id, so approval_id stays NULL until the effect authorizes and cancel does not withdraw an
  open gate. Proposed: a Cancels/Service hook to record approval_id at open.
- Request.IdempotencyKey is not stored (no column); the route's idem layer replays keys.
- Reconciliation book and client ledgers both come from trust.trust_transaction at month end
  (the trust journal); bank is the imported statement's closing balance. Proposed follow-up:
  read the book from the general-ledger trust bank control account so book vs ledgers is an
  independent check. Prepare re-prepares an open draft/ready month in place; a signed or
  imported month is ErrPeriodLocked. ImportStatement refuses with ErrNoParser.

## BOOT-REST-2b

- T-031.4 reminder worker (kernel/deadline/remind.go) added here; BOOT-21 jobs live in app/legal/boot/services_rest2.go (one boot file) instead of jobs_deadlines.go. The sent log defaults to in memory, so a restart can repeat that day's reminder once; a Postgres SentLog is open. No Postgres deadline Lister exists yet, so RestPorts.Deadlines takes it from the composition root.
- The Ontario holiday calendar counts business days left only while deadline.ontario_pack is on.
- HTTP-25: no RunService implementation (run starter + store) exists on any branch. HTTPPorts.Runs/RunAccess mount the routes when the composition root supplies them; until then they answer 503.
- HTTP-26 webhook row_version: not done (time limit). Needs an additive migration on platform.webhook and the Postgres reads in kernel/webhook to compare it for If-Match.
- STMT-IMPORT: recon.CSVParser (generic kind,date,description,amount_cents CSV, integer cents only) and recon.PGImports replace NoParser and PGService's import refusal at boot. Touched recon/http_write.go to put a ParseError's line and reason (never file text) in the 422 problem detail. Line descriptions go in StatementLine.Reference (the frozen contract has no description field).
- Merge fixes: recon read/write HTTP files declared the shared problem codes, Access and reconView twice (kept the read file's, Version int64); trust.TxRunner declared twice (kept receipt_http.go's).

## Q-HTTP-15 Gated payment routes: proposed allocations and refund gate

- POST /payments/{payment_id}/allocations: the sum must equal the payment (422
  allocation_sum_mismatch, ARC-I-029). A set of operating bills only, each named once, is
  proposed directly (200, allocation state `proposed`) through a new `payment.Proposals`
  port; the route declares `payment.allocation=proposed` and never
  `payment.allocation=applied`. Unapplied money (client credit) or a trust_ledger row opens
  billing.payment_allocation (202). The payment version is 1, as GET reports it.
- POST /payments/{payment_id}/refunds opens billing.refund (202) with payee = the payment's
  payer of record and reason = reason_code; an amount above the payment answers 409
  refund_exceeds_payment. The billing.refund effect (provider call keyed <gate_id>:<version>)
  and its registration are not built yet: an approved refund pays nothing until they are.
- Boot must wire `payment.Gated` (DB, Gates, Proposals and the HTTP dependencies); until it
  does, both routes answer 503.

## PG-WRITES-FIX

- Additive: migration `5505_withdrawal_gate.sql` adds nullable `trust.withdrawal_request.gate_id`
  (5504 is taken by another task branch). The request route stores the opened gate in the same tx;
  cancel withdraws that gate; only the preparer (the user, or the user an agent acts for) may cancel.
- Additive: `withdraw.Cancels` gains `RecordGate`, and `withdraw.Row` gains `GateID` (not contract files).
- Open: no Postgres `withdraw.FeeBills` adapter exists yet (billing owns the bill balance), so
  `Effect.Bills` is unwired at boot and every fee withdrawal fails closed at authorization.
- Open: the route opens `billing.trust_fee_withdrawal` for a bill withdrawal, but that kind has no
  effect wired (boot/unwired_gates.txt) and execution reads only `trust.withdrawal` approvals.
- Open: `PGService.Request` opens no gate, so the agent tool's `w.GateID == ""` check refuses every
  agent withdrawal on Postgres until the gate is opened in the service path.

## Q-BOOT-REST-2a payment.Gated, refund stores, WO-FIX close path, fee withdrawals

- Additive (CONTRACT kernel/registry/types.go): `GateKind.OnReject RejectFunc`, `Rejected` and
  the `RejectHook` interface. The PG engine and gatefake call OnReject in the deciding tx right
  after a decision rejects a gate; an error rolls the decision back. Nil does nothing. Boot sets
  it through a registry wrapper (`rejectFilter`), so gates.RegisterWired is unchanged.
- WO-FIX: billing.credit_note gets `credit.RejectHooks(PGWriteOffs{})`: a rejected write-off
  gate closes its draft as `rejected`. New route POST /credit-notes/{credit_note_id}/withdraw
  (additive in api/openapi.yaml, operationId withdrawWriteOff): only the preparer, as a person,
  withdraws the pending G-B5 gate and the draft closes as `withdrawn`.
- payment.PGRefunds (RefundStore) and payment.PGTrustRefunds (TrustRefunds) are the RestPorts
  defaults. A trust refund is a `client_refund` trust.withdrawal_request whose trust.withdrawal
  gate the G-B6 approver prepares; payment `trust_withdrawal_id` replay is keyed by the gate.
- payment.Gated is wired when HTTPPorts.Payments and PaymentProposals are set. Open: no Postgres
  `payment.Proposals` exists, so production boot still needs that port.
- PG-WRITES-FIX follow-up: boot.PGFeeBills (billing.bill + bill_balance, balance net of reserved
  fee transfers, bill row locked) is the default withdraw `Effect.Bills`. `withdraw.FeeEffect`
  handles billing.trust_fee_withdrawal: By-law 9 flag, then Effect.Apply (agent and preparer
  never approve, signatory check, bill re-check, ledger lock). Execution stays with the
  execution route: `PGApprovals` now accepts approved payloads of either gate kind, so the
  executor is still a second person (sole-practice exception audited). `PGService.Gates`, set
  at boot, opens the gate in the request tx and stores gate_id; the route skips opening when
  the service already did, so agent withdrawals are gated on Postgres.

## BOOT-REST-2b

- T-031.4 reminder worker (kernel/deadline/remind.go) added here; BOOT-21 jobs live in app/legal/boot/services_rest2.go (one boot file) instead of jobs_deadlines.go. The sent log defaults to in memory, so a restart can repeat that day's reminder once; a Postgres SentLog is open. No Postgres deadline Lister exists yet, so RestPorts.Deadlines takes it from the composition root.
- The Ontario holiday calendar counts business days left only while deadline.ontario_pack is on.
- HTTP-25: no RunService implementation (run starter + store) exists on any branch. HTTPPorts.Runs/RunAccess mount the routes when the composition root supplies them; until then they answer 503.
- HTTP-26 webhook row_version: not done (time limit). Needs an additive migration on platform.webhook and the Postgres reads in kernel/webhook to compare it for If-Match.
- STMT-IMPORT: recon.CSVParser (generic kind,date,description,amount_cents CSV, integer cents only) and recon.PGImports replace NoParser and PGService's import refusal at boot. Touched recon/http_write.go to put a ParseError's line and reason (never file text) in the 422 problem detail. Line descriptions go in StatementLine.Reference (the frozen contract has no description field).
- Merge fixes: recon read/write HTTP files declared the shared problem codes, Access and reconView twice (kept the read file's, Version int64); trust.TxRunner declared twice (kept receipt_http.go's).

## EVENTS-WIRE: event emitters at boot, walled-matter publisher (partial)

- Merges DL-EVENTS, EVENTS-2, DRAFT-FIX, CON-03-MEMO and BOOT-REST. Integration fix in
  app/legal/boot/agent.go (outside the listed files): FEAT-CORE-15's handlers read
  `matter.Query`, `agent.DocumentLookup`, `agent.UnbilledWIP` and `*agent.WriteTools`, so
  AgentPorts gains Matters, DocLookup, UnbilledWIP and WriteTools; JobBindings binds F-CON-02
  and F-CON-03; the action route maps the conflict-results and conflict-clearance screens to
  subject `conflict.run` with `conflicts.run`.
- New app/legal/boot/events.go: `newEventStores` builds PGBook, FilingStore and
  EmailFilingStore on the kernel emitter (RestServices gains Emails); `setCheckEvents` sets
  `conflict.Checks.Events` in buildClearance.
- `roles.walled_matter_accessed`: `WallResolver.WithEvents(em)` publishes once per wall check
  of a walled matter (allowed or refused, body ids plus `allowed`), outside a tx. It needs the
  wall store to implement `roles.WalledStore` (Walled(matterID)); the matter wall store does
  not yet, and boot does not build a WallResolver, so nothing calls WithEvents yet.
- Left (not done in this pass): HTTP routes calling PGBook.ChangeTrigger, IssueClaim,
  RecordAffidavitOfService and FileEmail (plus openapi entries); publishers for
  `identity.portal_submission` (no portal identity-evidence write point exists) and
  `party.organization_client_created` (no write point turns an organization into a client;
  `party.ClientOfLead` only reads); draft_document `subject_ref`.

## Q-EVENTS-WIRE-2 Routes for the evidence write points, draft subject_ref, wall and intake/party publishers

- Additive openapi paths (kernel/contract/additive.txt): `POST /deadlines/{deadline_id}/trigger`
  (PGBook.ChangeTrigger, `deadlines.edit`), `POST /filings/{filing_id}/issue` (IssueClaim),
  `POST /filings/affidavits-of-service` (RecordAffidavitOfService) and
  `POST /emails/{comm_item_id}/filings` (FileEmail), the last three on `documents.edit`. Each
  POST needs Idempotency-Key and answers problem+json. Module placeholders answer 503
  (`deadlines.Register` joins app/legal/modules.go; comms adds `registerFilingRoutes`); boot
  mounts the wired handlers from `HTTPPorts.Events`, built on the kernel DB and emitter.
  The trigger and issue routes check the capability firm-wide: the matter is only known after
  the store reads the row. The affidavit and email routes also check the body's matter, so a
  walled matter is 404.
- `draft_document` gains optional `subject_ref` (string, max 200) in spec/eng/a2-interfaces.json
  and agent/schemas/tools.json; it is kept in the draft's scope JSON. The F-CON-03 prompt sets it
  to the conflict run ref.
- `matter.PGWallStore` implements roles.WallStore and roles.WalledStore on matter.ethical_wall*;
  boot builds `roles.NewWallResolver(...).WithEvents(...)` (RestServices.Walls). The kernel
  emitter needs a tx and the wall check has none, so boot wraps it in `ownTxEmitter`, which
  opens its own tx when given nil.
- `party.organization_client_created`: party.Store.Create publishes it in the insert tx when
  the contact is an organization client (Store.Events, set at boot). `identity.portal_submission`:
  intake.Leads.Submit publishes it once per new lead (Leads.Events; a repeated key publishes
  nothing). Leads has no tx, so boot hands it the own-tx emitter (RestServices.LeadEvents); boot
  does not build Leads yet (no Postgres FormStore/LeadStore).
- Outside the listed files: app/legal/register_test.go (module count 36 -> 37 for deadlines),
  the F-CON-03 ok fixture prompt_hash and the regenerated plugin skill (prompt change).

## Q-EVENTS-WIRE-3 Matter-level checks on the evidence routes, intake leads at boot, matter publishers

- Matter-level access: `POST /deadlines/{deadline_id}/trigger` reads the deadline
  (`TriggerChanger.Get`, PGBook.Get) and checks `deadlines.edit` on its subject;
  `POST /filings/{filing_id}/issue` reads the filing's matter (`FilingWrites.MatterOfFiling`,
  new `FilingStore.MatterOfFiling`) and checks `documents.edit` on it. A walled matter answers
  404, a missing capability 403, as the affidavit and email routes already did. Table tests
  cover a walled user, a user without the capability and an allowed user on all four routes.
  The interface additions are additive (one new method each).
- Intake leads: `RestPorts.Leads` (`LeadPorts{Forms, Leads}`) makes boot build `intake.Leads`
  on the event-emitting contact store, publishing identity.portal_submission through
  `RestServices.LeadEvents` (`RestServices.Leads`). No Postgres FormStore/LeadStore exists and
  intake.lead has no idempotency key column, so the stores come from the caller (as
  RefundPorts does). Open: a Postgres LeadStore needs an additive migration for the key.
- matter.opened / matter.stage_changed: no Postgres write point opened a matter or changed its
  stage (Lifecycle.Transition and StageList.Move only compute). Added minimal ones:
  `matter.Store.Open` (pending -> open, open_date) and `matter.Store.ChangeStage` (moves
  stage_id and appends matter.stage_change in one statement), each emitting in its tx through
  `Store.Events`. Nothing calls them yet: the matter.opening gate effect and a stage route
  should; boot does not yet set `Store.Events`.
- Not done (time): document.revision_requested (docs has no revision request write point),
  court_form.changed (no court form store in app/legal or kernel) and email.received (comms
  has no inbound write point). None of the three is in spec/eng/a2-interfaces.json; each
  needs its write point and catalog entry decided first.

## Q-TAX-API (task MATTER-TAX-API) Matter court and property provinces and firm tax registrations over the API

Done in MATTER-TAX-API (decision Q-PG-BILL-FIX, TAX-FIELDS follow-up). Additive only:
- `PATCH /matters/{matter_id}` accepts `court_province` and `property_province`, plain fields written at
  the If-Match version like `short_name`. A value must be a Canadian province or territory code
  (`matter.ProvinceCodes`, upper case; `EX` is refused) or `""` to clear it. `matter.PlainChange` gains
  `CourtProvince` and `PropertyProvince` (additive contract fields); the matter view returns both.
- `GET` and `PATCH /firm/tax-registrations` (`tax.RegistrationsHTTP`, `tax.PGRegistrations`) read and
  replace `core.firm.tax_registration_provinces`: unique province or territory codes, stored sorted.
  Reading needs `billing.view`; changing needs `billing.approve`, since the list decides which
  provincial tax a bill charges. Open: is a firm-settings capability (e.g. `admin.firm_settings`) wanted?
- ARC-I-006 needs an integer version for the PATCH: `migrations/5624_firm_tax_registrations_version.sql`
  adds `core.firm.tax_registrations_version integer NOT NULL DEFAULT 1` (billing block, as 5623).
- openapi: `/paths/~1firm~1tax-registrations` and the schemas `ProvinceCode`, `PlaceOfSupplyPatch` (the two
  matter fields) and `FirmTaxRegistrations`, each listed in `kernel/contract/additive.txt`.
- Open: the matter PATCH has no `requestBody` in openapi, and adding one cannot be listed: its pointer
  `/paths/~1matters~1{matter_id}/patch/requestBody` puts the legal term `matter` into
  `kernel/contract/additive.txt`, which `TestKernelHasNoLegalTerms` refuses, and `tools/kernel_allow.txt`
  is a ratchet that may not grow (`TestKernelRatchet`). Decide: exempt additive.txt from the term scan (it
  lists app paths by design), or move the approval list out of `kernel/`. Then add the requestBody as
  `allOf` of the plain fields and `PlaceOfSupplyPatch`.
- Not wired at boot: no Postgres `matter.Updater` exists yet and boot mounts neither `matter.Writes` nor
  `tax.RegistrationsHTTP`. The boot follow-up wires both (and the Updater writes the two columns).
- Resolved by TOOL-FIX-C: when the run scope declares a gate kind on work items
  (`agent.WorkItemGateKinds`: `work.absence`, `work.output`), `create_task` returns its task
  (still proposed, no approver) as an `OutcomeDraft` of the run, so `submit_for_approval` can
  open the gate on it. `TestFDL09_FromFeature` now checks the pending gate, the 403 for the
  agent, `ErrSelfApproval` for the acting user and another lawyer's approval; a Def without
  `work.absence` still gets a proposal and the submit is refused.

## Q-TOOL-FIX-C propose_deadline input gains deadline_id (additive a2 change)

- Task: TOOL-FIX-C (FEAT-TOOL-15, FEAT-TOOL-16 follow-up; closes Q-FEAT-DL-09, Q-FEAT-DL-10)
- `propose_deadline`'s input schema gains an optional `deadline_id` (string) in
  `spec/eng/a2-interfaces.json` and its copy `app/legal/agent/schemas/tools.json`. Additive:
  every input valid before is still valid. The cal.change payload's `reason` is the input
  `title`; there is no separate reason field.
- `propose_deadline` now also reads a `DeadlineGetter` dep (kernel/deadline `Get`), which the
  deadline engine and its fakes already implement.
- `work.output` is counted as a work-item gate kind beside `work.absence` because its payload
  names an `item_id`; confirm when the F-WRK features that use it are built.

## Q-FEAT-CON-04 Conflict feature tests: stand-ins, test helpers and files beyond the task list

- Tasks: FEAT-CON-04, FEAT-CON-05, FEAT-CON-06 (F-CON-04 to F-CON-06).
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (each Def in
  appDefs, as FEAT-CORE-17 asks) and the generated plugin skills under
  `paploo/counsel-plugin/skills/f-con-0N-*/SKILL.md`.
- The task lists `app/legal/conflicts/ai_f_con_04_test.go`; the landed AI-036 test lives in
  `app/legal/conflict/` (package `conflict`), so it was moved to FromFeature there.
- The agenttest Seed's Deps carry no `agent.SubjectMatters`, so `submit_for_approval` would run
  as a stand-in, and its draft ids are not a2 uuids. The conflict feature tests add both through
  `conHandlers` / `conDrafts` in `ai_f_con_04_feature_test.go` (reused by F-CON-05 and F-CON-06).
  Proposed: a `SubjectMatters` and uuid draft ids in `agenttest.Seed`.
- `search_matters` and `read_document` have no `Handler`, so they still register as stand-ins.
  The ok scripts call neither; the injection scripts call `read_document` to carry the injected
  text, since `agenttest.QuotedOnly` reads only a top-level `agenttools.Quoted` result and
  `search_contacts` returns its quotes under `quotes[]`.
- Defs set `Grant` to the capabilities of their tools: an empty grant refuses every call.
- The event types named here (`conflict.clearance_subject_to_consent`, ...) have no publisher yet.
- F-CON-05: `propose_conflict_parties` opens no kernel gate. Its G2 is the open conflict run
  that `conflict.Clearance` decides through app/legal/gates, so the F-CON-05 test checks the
  run is open and prepared by the acting user, and that the agent (`conflict.ErrAgentApproves`)
  and the acting user (`conflict.ErrPreparerApproves`, the G2 self-approval refusal; kernel
  `gate.ErrSelfApproval` does not apply) are refused with 403. `submit_for_approval` cannot open
  conflict.clearance on a run: the run scope only records drafts, not proposals.

## Q-FEAT-CON-07 Conflict features F-CON-07 to F-CON-09 on top of FEAT-CORE-15

- Tasks: FEAT-CON-07, FEAT-CON-08, FEAT-CON-09. Same layout as Q-FEAT-CON-04: each Def joins
  `featureset.appDefs` and its test; tool-use Defs get their generated plugin skill.
- The branches merge `task/FEAT-CORE-15`, which gives `read_document` and `search_matters` real
  handlers. The conflict scripts and the F-CON-04 fixtures read a fixed doc id without
  `version_id` through `SetRead`, so `conHandlers` (in `ai_f_con_04_feature_test.go`) now leaves
  both tools out of the handler set and they stay stand-ins. Proposed follow-up: seed the
  documents with `agenttest.Seed.Document`, pass `version_id`, and check the injection with a
  helper that accepts read_document's `{"untrusted": true}` envelope (F-CON-02 has one,
  `conQuotedOnly`).
- `task/FEAT-CON-02`/`03` and `task/FEAT-CON-04`..`06` both declare `conFeature`, `conScript`,
  `conCall` and others in package `conflict_test` with different signatures; merging both
  chains will not compile until one set is renamed.
- F-CON-07's model call is extraction (spec): its Def sets `Kind: ai.KindExtraction`, so it
  exports no plugin skill.
- Event types with no publisher yet: `identity.portal_submission` (F-CON-07),
  `party.organization_client_created` (F-CON-08), `roles.walled_matter_accessed` (F-CON-09).
- F-CON-09's "schedule: review dates" is a per-wall date, so its binding checks daily
  (`trigger.ParseCadence`) and needs the acting user boot names, like F-CON-05. The F-CON-07
  and F-CON-08 gate-kind tests now use `conflict.GateWall` for the out-of-Def kind.
- Event types with no publisher yet: `identity.portal_submission` (F-CON-07).

## Q-FEAT-CON-10 Conflict features F-CON-10, F-CON-15 and F-CON-16

- Tasks: FEAT-CON-10, FEAT-CON-11, FEAT-CON-12. Same layout as Q-FEAT-CON-07: each Def joins
  `featureset.appDefs` and its test, and gets its generated plugin skill. The branches stack on
  `task/FEAT-CON-07` (which carries FEAT-CORE-15); `featureset.go` and its test take one line
  each per task, so they merge with FEAT-CON-08/09 by keeping both lines.
- F-CON-15 and F-CON-16 are screen actions. The spec words the button in `trigger`
  ("on request: ethical-walls > Propose wall") and leaves `on_request` empty, while the screen
  action route reads `Def.Actions`. The Defs set `Actions` to the trigger's button; their spec
  test checks that and compares the rest of the Def with `Actions` cleared. Proposed: fill
  `on_request` for screen-action features in `spec/ai/features.json`.
- Gate kind constants `GateIDDestruction` (G14), `GateWall` (G2b) and `GateLateral` (G2c)
  live in the Def files. `conflict.id_destruction` needs two approvers; the feature test checks only
  that another lawyer may decide, not that the batch is fully approved.
- F-CON-10's weekly binding needs an acting user from boot (the records officer); boot wiring
  is not in these tasks.
  `party.organization_client_created` (F-CON-08).

## Q-FEAT-CON-13 Read-only conflict features F-CON-11 to F-CON-14 and F-CON-17

- Tasks: FEAT-CON-13 (F-CON-11), FEAT-CON-14 (F-CON-12), FEAT-CON-15 (F-CON-13, F-CON-14,
  F-CON-17). Each Def joins `featureset.appDefs` (outside the task's file list); chat and
  embedding Defs export no plugin skill.
- The shared read-only harness lives in `ai_f_con_11_feature_test.go`: `search_contacts` and
  `read_document` run their real handlers over `agenttest.Seed`, with read_document's first
  page re-quoted as `agenttools.Quoted` so `agenttest.QuotedOnly` and `run.CheckAnswer` see
  its citation. `search_matters` stays a stand-in the scripts never call (the Seed has no
  `matter.Query`).
- None of the three read tools returns a date as a JSON value, so the unsourced-date check
  counts a date inside a quoted document line as read in the run. Proposed follow-up: have
  read_document return `agenttools.Quoted` itself so the harness needs no re-quote.
- The agent-never-approves check goes through `conflict.Clearance` on a check the agent
  prepared for the acting user: agent principal -> `ErrAgentApproves`, acting user ->
  `ErrPreparerApproves`, both 403.

## Q-FEAT-DOC-03 QuotedOnly accepts a real read handler's untrusted output

- The document drafting features run read_document's real handler over Seed documents (no
  stand-ins). Its output is `{"untrusted": true, "doc_id", "version_id", "pages": [...]}`, not
  an `agenttools.Quoted`, so `agenttest.QuotedOnly` reported the quoted lines as unquoted.
- `agenttest/agenttest.go`: QuotedOnly now also accepts a tool_result marked untrusted at its
  top level whose string values hold the text (additive; the Quoted path is unchanged).
  `agenttest/quoted_test.go` covers both shapes and the refusals.
- F-DOC-02 binds both `conflict.cleared` and `lead.converted` (the spec trigger names G2 and
  G1); the prompt has the agent draft only when the matter shows both done.

## Q-EVENTS-WIRE-4 Intake Postgres stores, matter opening effect, five new events

- Merge of origin/main: boot gained the `DeadlineGet` agent port (propose_deadline needs
  `agent.DeadlineGetter`), `JobBindings` binds every job feature merged from main, and
  `actionScreens` lists their screens. F-DOC-06 binds weekly plus `court_form.changed`.
- Intake: `intake.PGForms` and `intake.PGLeads` (additive migrations 5300/5301: nullable
  `intake.lead.idempotency_key`, unique on (form_id, key) built concurrently). Boot always
  builds `intake.Leads` on them; `RestPorts.Leads` only overrides the stores.
- matter.opened: `matter.Store.OpeningEffect` applies a `matter.opening` gate whose payload is
  the PATCH status=open shape (pending -> open in the gate tx); boot wires it with the rest
  effects on a `matter.Store` whose `Events` is the kernel emitter (`EventStores.Matters`). The
  POST /matters create shape is refused by the effect (no create-and-open write point yet), so
  `matter.opening` stays in unwired_gates.txt for the base effect set. matter.stage_changed:
  `Store.ChangeStage` is the write point; no stage route exists yet to call it.
- New a2 events, added additively to spec/eng/a2-interfaces.json, each with a write point
  that emits in its own tx: `document.revision_requested` (`docs.Revisions.Request`, document
  back to draft), `court_form.changed` (`docs.CourtForms.Record`, new docs.court_form version),
  `email.received` (`comms.Inbound.Receive`, inbound comm_item, redelivery publishes nothing),
  `matter.note_added` (`matter.Store.AddNote`, matter.note row), `calendar_event.added`
  (`deadlines.CalendarEvents.Add`). Payloads carry ids only, never text. Boot builds each on the
  kernel emitter (`EventStores`). Open: no HTTP route calls them yet.
- F-CON-04 ok fixture rehashed: the merged branch tool schemas change its prompt hash; prompt text unchanged.

## Q-EVENTS-WIRE-5 Routes for the EVENTS-WIRE-4 write points, matter.opening creates and opens

- Additive openapi paths (api/contract-additive.txt), each POST with Idempotency-Key and
  problem+json, placeholder 503 until boot wires it (`HTTPPorts.Events`):
  `POST /matters/{matter_id}/stage` (`matter.Store.ChangeStage`, matter.stage_changed),
  `POST /matters/{matter_id}/notes` (`AddNote`, matter.note_added; the body is a `body_ref`,
  never text), both on `matters.edit`; `POST /documents/{document_id}/revision-requests`
  (`docs.Revisions.Request`, `documents.edit`); `POST /court-forms` (`docs.CourtForms.Record`,
  `documents.templates`, firm-wide); `POST /emails/inbound` (`comms.Inbound.Receive`,
  `comms.comm_item`, firm-wide: an inbound email has no matter until filed);
  `POST /calendar-events` (`deadlines.CalendarEvents.Add`, `deadlines.edit`).
- Matter-scoped routes (stage, notes, revision on a matter document, calendar event with a
  matter_id) check the capability firm-wide and then on the matter, as the EVENTS-WIRE-3 routes
  do: a walled matter answers 404, a missing capability 403. The shared flow is the new
  `app/legal/eventroute` package; the revision route reads the document's matter through the new
  `docs.Revisions.MatterOf`.
- The stage route is a person's direct move (approved_by is the acting user; an agent run is
  403). It does not open `matter.stage_move`; that gate's effect stays unwired. Open: whether a
  direct move should instead always go through `matter.stage_move`.
- matter.opening: POST /matters now requires `number`, chooses the new matter's id and puts
  `matter_id` and `practice_area` on the gate payload (the spec gate schema requires them, with
  `responsible_lawyer_id`); PATCH status=open adds `responsible_lawyer_id` and `practice_area`.
  `OpeningEffect` applies either shape in the gate tx: a status change opens the pending matter,
  the create shape inserts the matter already open. Both publish matter.opened. The effect
  refuses a gate with no approver, an agent approver, or an approver who is the preparer (or the
  user an agent run prepared it for). `matter.opening` leaves unwired_gates.txt; its effect is
  registered whenever boot has the kernel DB, emitter and clock (the unwired test kernel now has
  an emitter).

## Q-FEAT-DOC-02 F-DOC-01 drafts its filing review: draft_document added to the Def and spec

- FEAT-DOC-02 asks that the tool-gap kind docs.filing_privilege (G-D1) be opened "via
  draft_document then submit_for_approval". submit_for_approval opens a gate only for a
  subject the run drafted, and F-DOC-01 listed no draft tool, so its run could not open G-D1.
- As decided for F-DL-02 (Q-FEAT-DL-04), draft_document is added to F-DOC-01 in
  spec/ai/features.json and in IngestDocumentDef (app/legal/docs/ai_f_doc_01.go), with its
  capability documents.edit in the Grant. The change is additive; the extraction pipeline
  (IngestPipeline and its IngestSink) is unchanged.
- The four intake channels (upload, email-in, portal upload, scan) all file a document
  version, and a2 names one event for that, document.uploaded, so the binding has one event.

## Q-FEAT-DOC-04 F-DOC-03 trigger event

- "user instruction or task from the matter plan": the task half binds `work_item.created`;
  a user's instruction reaches F-DOC-03 through the ai-draft-workspace > Draft action.

## Q-JOB-ACTOR Scheduled AI features act for a designated lawyer (decided as counsel)

- A scheduled job feature acts for the lawyer the firm names for it in firm settings. The
  setting is a new table `agent.designated_lawyer (feature_id, user_id)` (migration 6201,
  additive, owned by app/legal/agent), not a column on core.firm, since a module writes only
  its own tables. `agent.PGDesignatedLawyers` reads and sets it; Set accepts only an active
  human lawyer in good standing, and a read of a user who no longer is answers "".
- `trigger.BindDesignated(def, events...)` binds a scheduled feature whose runs act for its
  designated lawyer, read at each tick (`Bindings.WithDesignated`). No lawyer named, or no
  settings source, means the run does not start (fail closed, logged as a warning), and
  `Bindings.Readiness` reports the feature as `no_designated_lawyer`. `Bind(def, actingFor)`
  with a fixed acting user still works for bindings that need one.
- F-DL-06 now binds with BindDesignated (`RuleWatchBinding()` takes no argument). At merge,
  the scheduled bindings on unmerged branches (F-CON-05 ReverseSweepBinding, F-CON-10
  ExpiryRetentionBinding, F-DL-05 DigestBinding) should switch the same way; until then they
  keep compiling with a fixed acting user. Boot wiring and a readiness HTTP surface are not
  built yet: boot calls `WithDesignated(agent.NewPGDesignatedLawyers(pool))` when it builds
  the Bindings, and surfaces `Readiness` on the setup checks.

## Q-FEAT-DOC-05 F-DOC-04 trigger event and gated subject

- "document enters the review queue" binds `document.draft_created`: a new draft is what
  enters the review queue.
- The spec gives F-DOC-04 only draft_document and submit_for_approval as writes, so the
  pre-check report is a draft the run submits under docs.outgoing (G8); the reviewing lawyer
  reads it beside the document under review.

## Q-FEAT-DOC-07 F-DOC-06 trigger events and test helpers

- "A known rule or form change" binds two events: `rulepack.published` (the kernel deadline
  event, reused as `docs.EventRulePackPublished`) and `court_form.changed`
  (`docs.EventCourtFormChanged`, proposed). Nothing publishes `court_form.changed` yet; the
  docs module emits it when a new docs.court_form version lands (EVENTS-WIRE follow-up).
- The weekly run binds with `trigger.BindDesignated` (JOB-ACTOR), so this branch merges
  task/JOB-ACTOR.
- The end-to-end tests use `app/legal/docs/ai_docc_helpers_test.go` (prefix `docc`), a file
  outside the task's list, shared byte-for-byte with FEAT-DOC-08 and FEAT-DOC-09 so the three
  branches merge cleanly. FEAT-DOC-06 already covered one G-D2 gate end to end; this task's
  test drives both gate kinds in one run.

## Q-FEAT-DOC-08 F-DOC-07 precedent-candidates: base and helpers

- Builds on task/FEAT-DOC-06 (the `prompts` embed and `GatePrecedent`) and task/JOB-ACTOR
  (the monthly run binds with `trigger.BindDesignated`); both are merged into this branch.
- The end-to-end tests use `app/legal/docs/ai_docc_helpers_test.go` (prefix `docc`), shared
  byte-for-byte with FEAT-DOC-07 and FEAT-DOC-09. The featureset files and the generated
  skill get one line or file for F-DOC-07.

## Q-FEAT-DOC-09 F-DOC-08 email-filer: event name and gate constant

- "Email arrives in connected mailbox" binds `email.received` (`docs.EventEmailReceived`,
  proposed), carrying the filed message's ids and the mailbox owner as user_id. No mailbox
  connector publishes it yet (EVENTS-WIRE follow-up).
- FEAT-DOC-01 (unmerged) exports `GateFilingPrivilege`; to merge cleanly beside it this task
  names the kind with the unexported `emailFilerGate`. Once both are on main, the Def can use
  GateFilingPrivilege and the private constant can go.
- The end-to-end tests use `app/legal/docs/ai_docc_helpers_test.go` (prefix `docc`), shared
  byte-for-byte with FEAT-DOC-07 and FEAT-DOC-08. F-DOC-08 is an extraction Def, so it gets
  no plugin skill.

## Q-FEAT-DOC-10 Document features on real reads (F-DOC-09, F-DOC-10, F-DOC-11)

- The branch merges `task/AGENTTEST-FIX` for its envelope-aware `agenttest.QuotedOnly`: the feature tests
  run read_document's real handler over `Seed.Document` (with its version_id), whose result is
  `{"untrusted": true, "pages": ...}`, not an `agenttools.Quoted`.
- The shared test helpers carry the `docd` prefix (`ai_f_doc_09_feature_test.go`); FEAT-DOC-11 and
  FEAT-DOC-12 stack on this branch and reuse them.
- Gate kind constants are per feature (`GateCourtFormRepublish`, ...) because other unmerged DOC
  branches declare `GateTemplatePublish` and `GateOutgoing`; the integrator may fold them into those.
- F-DOC-09 is scheduled (daily): `CourtFormCurrencyBinding(actingFor)` takes the designated lawyer, as
  `deadlines.RuleWatchBinding` does; it can move to `trigger.BindDesignated` once JOB-ACTOR merges.
- None of these Defs' write tools is a sourced write (draft_document carries no amount or date
  pointers), so `agenttools.ErrUnsourcedValue` cannot arise in their runs; no test asserts it.
- F-DOC-10 binds `matter.opened` and `matter.stage_changed`; the matter module does not publish either
  yet (add them to EVENTS-WIRE).
- F-DOC-11 binds `document.revision_requested`; no module publishes it yet (add it to EVENTS-WIRE with the
  revise request route).

## Q-FEAT-DOC-16 F-DOC-19 request documents from client

- Files beyond the task list: `app/legal/agent/featureset/featureset.go` and its test register
  `docs.RequestDocumentsDef()`, and `go generate` added its plugin skill.
- F-DOC-19 is a screen action: the spec words its button in the trigger and leaves
  `on_request` empty, so the Def's `Actions` hold that button and the spec sync test checks
  them against the trigger (as F-CON-15 does).
- The request goes out as a draft email; `submit_for_approval` on the `email_draft` subject
  opens `platform.portal_message`. `draft_email` is called without `submit`, so it never opens
  `platform.outgoing_email`, which is outside the Def's gate kinds.

## Q-FEAT-DOC-17 F-DOC-05 citation-staleness

- Files beyond the task list: the featureset registration of `docs.CitationStalenessDef()`
  and its generated plugin skill. The branch builds on task/FEAT-DOC-16 for the shared
  `docf` test helpers.
- a2 has no "new version" event; a new version is a `document.uploaded` (it carries
  `version_id`), so F-DOC-05 binds that event. The prompt has the agent flag only drafts that
  cite an older version.
- The flags are the run's extraction output (`OutputSchema`: ranked items with a citation);
  there is no write tool, so nothing is stored and no gate opens.

## Q-FEAT-DOC-18 F-DOC-12 place signing fields

- Files beyond the task list: the featureset registration of `docs.PlaceSigningFieldsDef()`.
  The branch builds on task/FEAT-DOC-17 for the shared `docf` test helpers.
- F-DOC-12 is a read-only screen action: the proposed fields are the run's extraction output
  (`OutputSchema`: items in signing order with signer role, field, template anchor or detected
  block, and a citation). No write tool places them; the user does, so no gate opens.

## Q-FEAT-DOC-20 F-DOC-13 screen action binding and run write path (files outside the task)

- `app/legal/docs/ai_f_doc_13.go` (FEAT-DOC-19): `AutoFileDef` gains
  `Actions: []string{ActionAutoFile}` and its trigger is built from that constant, so
  `POST /api/agent/actions/F-DOC-13` from matter-documents queues a run (the route only
  accepts a screen listed in `Def.Actions`). features.json has `on_request: []` for the
  read-only screen actions and names the action only in `trigger`.
- `app/legal/docs/ai_doc_helpers_test.go` (FEAT-DOC-06): the spec sync check reads the
  on-request actions from `on_request`, else from the `on request: ` trigger (`docOnRequest`).
- `AutoFileFromRun` (ai_f_doc_13_write.go) is the screen-action write path: the run's
  output reaches the FilingSink only when every citation's page and line numbers came from
  the run's reads (Provenance, `ErrUnsourcedValue`) and fall inside a citation the run
  returned (`run.ErrUncitedAnswer`). It opens no gate and calls no write tool.
- The shared `docg*` test helpers for the read-only document screen actions (F-DOC-13,
  F-DOC-18, F-DOC-23) live in `ai_f_doc_13_feature_test.go`; FEAT-DOC-21 and FEAT-DOC-22
  build on this branch.

## Q-FEAT-MAT-01 Brief refresh events; shared matter AI helpers

- a2 names only `document.uploaded` of F-MAT-01's trigger ("document, email, note or event
  added to a matter"). The Def also binds `email.filed`, `matter.note_added` and
  `calendar_event.added` (app/legal/matter/ai_f_mat_01.go), which the owning modules do not
  publish yet.
- No F-MAT-01 write carries an amount or a date field, so ErrUnsourcedValue has no case. The
  feature test covers the draft tool's source check instead: a key-fact draft citing a
  version the run did not read is refused with agent.ErrUncitedSource.
- FEAT-MAT-06, -10 and -01 share one test helper file, app/legal/matter/ai_mat_helpers_test.go
  (mat prefix), so task/FEAT-MAT-10 is based on task/FEAT-MAT-06 and task/FEAT-MAT-01 on
  task/FEAT-MAT-10.

## Q-FEAT-MAT-02 F-MAT-03 stage advance watcher

- Besides the listed files, `app/legal/agent/featureset/featureset.go` gains
  `matter.StageAdvanceDef()` and `go generate` adds its plugin skill
  (`paploo/counsel-plugin/skills/f-mat-03-stage-advance-watcher`), as every feature does.
- The spec's "evidence event (signed, filed, paid, deadline passed)" has no event of its own in
  spec/eng/a2-interfaces.json, so the binding uses the nearest a2 events: `document.released`
  (signed), `document.uploaded` (filed), `payment.received` (paid) and `deadline.met`
  (deadline passed). A dedicated e-sign or deadline-passed event can be added to
  `matter.StageEvidenceEvents` when one exists.
- `matter.stage_move` is a tool-gap kind: the run drafts the proposal with draft_document and
  opens G-ST with submit_for_approval on that draft. The feature tests answer
  `agent.SubjectMatters` from the seed's drafts (agenttest.Seed.Deps has none).

## Q-FEAT-MAT-03 F-MAT-04 party extraction

- Besides the listed files, `app/legal/agent/featureset/featureset.go` gains
  `matter.PartyExtractionDef()`. It is an extraction Def, so no plugin skill is generated.
- As in Q-FEAT-CON-01, propose_conflict_parties opens no kernel gate: it runs the matter's
  conflict check, and the open check run is the pending matter.party_conflict (G2). The tests
  assert the run is open with the acting user as preparer, an agent decider is 403
  (`gates.ErrAgentApproves`) and the acting user is refused as preparer
  (`gates.ErrPreparerApproves` / `conflict.ErrPreparerApproves`, the app's self-approval
  refusal), not the kernel `gate.ErrSelfApproval`.
- The spec trigger "new pleading, correspondence or closing document" binds to the a2
  `document.uploaded` event (`matter.PartyDocumentEvents`); a2 has no email event yet.
- The branch is based on task/FEAT-MAT-02 for the shared matb test helpers.

## Q-FEAT-MAT-04 F-MAT-05 contact dedupe and enrichment

- Besides the listed files, `app/legal/agent/featureset/featureset.go` gains
  `matter.ContactDedupeDef()`. It is an embedding Def, so no plugin skill is generated.
- The daily schedule binds with `trigger.BindDesignated` (Q-JOB-ACTOR): scheduled runs act for
  the feature's designated lawyer; `contact.created` runs act for the event's user. The branch
  is based on task/FEAT-MAT-03, which carries task/JOB-ACTOR and the shared matb helpers.
- `matter.contact_merge` is a tool-gap kind: the run drafts the proposal with draft_document
  and opens G-CM with submit_for_approval on that draft.

## Q-FEAT-MAT-12 F-MAT-14's "weekly sweep" has no "schedule" word

- spec/ai/features.json words F-MAT-14's trigger "event: matter opened without a template;
  weekly sweep". trigger.ParseCadence finds a schedule only where the trigger says
  "schedule" (or "monthly"), so it reads this trigger as CadenceNone and BindDesignated
  refuses it.
- FEAT-MAT-12 keeps Def.Trigger equal to the spec and has TemplateFitBinding bind a copy of
  the Def whose trigger reads "schedule: weekly sweep" (weekly, designated lawyer), plus
  the matter.opened event. app/legal/agent/trigger is not in the task's files.
- Proposed: ParseCadence also treats "sweep" as a schedule word; the binding then uses the
  Def unchanged. "Without a template" is checked by the run (get_matter), since
  matter.opened carries no template field.

## Q-FEAT-MAT-13 propose_conflict_parties and create_task return proposals, so their gates open outside the run

- Tasks: FEAT-MAT-13 (F-MAT-16), FEAT-MAT-14 (F-MAT-17), FEAT-MAT-15 (F-MAT-18). Same gap as Q-FEAT-DL-10.
- `propose_conflict_parties` and `create_task` return `OutcomeProposal`, which the run scope does not record as a draft,
  so an in-run `submit_for_approval` for `matter.party_conflict` or `work.plan` is refused with `ErrNotFound`.
- The FromFeature tests check the real proposal (the open re-run check, prepared by the acting user, or the proposed task),
  then open the Def's gate on it as the acting user and check: pending, agent principal 403, acting user `ErrSelfApproval`.
- Proposed: record a proposal as a run draft when the run scope allows the Def's gate kind (FEAT-TOOL-16/19), so the agent
  can submit it in the run.

## Q-INTEG-16-SCREENS Action screens and job bindings for the pass 16 document and matter features

- The action route's screen map adds, as matter subjects: document-assembly, client-sharing
  and signature-request (documents.edit); email-filing, bundle-builder,
  multi-document-analysis, document-review, matter-documents and document-search
  (documents.view); matter-overview, matter-chronology, matter-stage-board,
  matter-financial-summary, matter-open-wizard, matter-parties and practice-area-settings
  (matters.view); matter-notes-activity (matters.edit). template-library (documents.edit),
  precedent-library and firm-library (documents.view) select a docs.document;
  contact-list and contact-detail select a party.contact (contacts.view). Open: confirm
  practice-area-settings (a firm setting, not a matter record) and matter-open-wizard (no
  matter yet) should select another subject kind.
- JobBindings: F-CON-05, F-CON-10 and F-DL-05 now act for their designated lawyer
  (JOB-ACTOR), and so do F-DOC-06 (TemplateCurrencyBinding, which also starts on
  court_form.changed), F-DOC-07, F-MAT-05, F-MAT-09 and F-MAT-14. F-DOC-09 still takes the
  rule admin; open: switch CourtFormCurrencyBinding to BindDesignated. F-MAT-12 binds to
  document.uploaded for the event's user (ChronologyBinding).
- Later pass 16 merges (F-MAT-02, 10, 11, 13, 15, 17 to 23): firm-home, matter-access,
  matter-access-report, matter-ask, global-search, matter-list, matter-templates and
  firm-feed select a matter (matters.view); contact-import and contact-field-settings select a
  party.contact (contacts.view). Open: firm-home, firm-feed, global-search and the settings
  screens have no single matter record. F-MAT-13 is on request only, so JobBindings lists it
  as served by its screen actions instead of binding it.

## Q-FEAT-TRU-06 F-TRU-06 schedule, screen subject and sourced worksheet amounts

- "schedule (after fiscal year end)" names no cadence, so trigger.ParseCadence checks daily.
  F-TRU-06 binds with trigger.BindDesignated (the firm's designated lawyer acts) and its
  prompt writes nothing until the fiscal year has ended. Open: a yearly cadence keyed to the
  firm's fiscal year end.
- Main has no boot.FirmSubject, so the firm-level lso-annual-report screen selects matters
  (SubjectKind matter, capability trust.view) in boot actionScreens.
- draft_document carries its amounts in body_markdown, which the Provenance field pointers
  cannot reach. app/legal/trust/ai_sourced.go adds CheckSourcedCents and SourcedDraft: every
  "N cents" figure in an agent's draft must be one a read returned in the run, and a dollar
  figure, a fraction of a cent or a missing Provenance fails closed (ErrUnsourcedValue). The
  feature tests wrap draft_document and draft_email with it. Open: boot registers one
  draft_document and one draft_email for every feature, so SourcedDraft is not yet applied to
  live trust runs; proposed: boot wraps the draft tools with trust.SourcedDraft for runs of a
  trust-area Def (or the draft tools gain an amounts field the Provenance pointers cover).

## Q-FEAT-TRU-07 F-TRU-07 screens and the top-up amount

- client-trust-ledger is one matter's ledger and selects the matter; retainer-coverage is a
  firm-level screen and, with no boot.FirmSubject on main, also selects matters (trust.view).
- The request asks for the unpaid balance of the delivered bills, a figure get_bill
  returned, never a worked-out shortfall: trust.SourcedDraft refuses any cents figure the run
  did not read. Open: a sourced "agreed retainer floor" read (engagement terms) would let the
  request name the floor too; get_matter does not return it today.

## Q-FEAT-TRU-09 F-TRU-09 screen subject and gate path

- trust-audit-trail is a firm-level screen; with no boot.FirmSubject on main it selects the
  matters the pack covers (SubjectKind matter, capability trust.view).
- draft_document opens no gate itself, so the pack's docs.outgoing (G-REL) gate opens through
  submit_for_approval on the draft, and submit_for_approval refuses any other kind
  (ErrGateKindNotAllowed). Amounts are checked by trust.SourcedDraft (Q-FEAT-TRU-06).

## Q-FEAT-TRU-01 bank-feed-sync: screens, binding and files outside the task list

- Registering F-TRU-01 in featureset.App() (FEAT-CORE-17) makes the boot tests require its
  screens and a job binding, so the task also touches app/legal/agent/featureset and
  app/legal/boot/agent.go.
- trust-receipts-journal and bank-feed-import are firm-level screens. main has no
  boot.FirmSubject yet, so both select a matter (trust.view). Open: switch them to the firm
  subject once it lands.
- The schedule ("daily, plus when a statement is uploaded") binds with the write path in
  FEAT-TRU-02; until then JobBindings lists F-TRU-01 as served by its screen actions.
- The pipeline takes every amount and date from the cited bank-feed line: an amount is
  integer cents ("125000 cents") or dollars and cents ("$1,250.00") in the line, else
  agenttools.ErrUnsourcedValue and nothing is written. Its ReceiptProposals sink (opening
  trust.receipt_classification with the acting user as preparer) is wired in FEAT-TRU-02.

## Q-FEAT-TRU-04 month-end-reconciliation: trigger event, gate kind and amount provenance

- No kernel event names a received bank statement. F-TRU-04 binds to the new
  `trust.statement_received` (trust.EventStatementReceived) plus a daily check (the spec's
  "schedule"), acting for the firm's designated lawyer (BindDesignated, JOB-ACTOR). Open: the
  module that files statements publishes the event.
- recon imports trust, so trust restates G12 as trust.GateReconciliationSignOff; a test holds
  it equal to recon.GateSignOff.
- draft_document and draft_email check citations but not amounts. trust.SourcedCents wraps
  them so a trust write may state only integer cents and dates a read of the run returned
  (signed, so an overdraft cannot be written unsigned), else agenttools.ErrUnsourcedValue and
  no draft or gate. The FromFeature tests wire it; open: boot wraps the draft tools of trust
  feature runs with it (the shared handler set has no per-feature wrapping yet).
- monthly-reconciliation is a firm-level screen and selects a matter until boot.FirmSubject
  lands (Q-FEAT-TRU-01).

## Q-FEAT-TRU-05 retainer-replenishment: trigger event and the shortfall's figures

- No kernel event names a ledger below its retainer floor. F-TRU-05 binds to the new
  `trust.ledger_below_floor` (trust.EventLedgerBelowFloor), acting for the event's user.
  Open: the trust module publishes it when a posting takes a ledger below the floor agreed
  in the engagement.
- The shortfall is Counsel's figure, never the model's: trust.NewRetainerShortfall keeps the
  balance signed, flags an overdraft and covers it (floor - balance), and Observe records
  its figures into the run's provenance before the run starts (fails closed without one).
  Open: the event handler calls it and passes Prompt() as the run's request.
- agent imports trust, so trust restates G-OUT as trust.GateOutgoingEmail; a test holds it
  equal to agent.GateOutgoingEmail.

## TRUST-DRAFT-WRAP trust drafts are checked in production (decided)

- Decided as counsel (trust exception): trust drafts carry figures that go to the regulator
  or to clients, so every trust feature's draft_document and draft_email runs behind the one
  trust sourced-figures check, trust.SourcedCents. It merges TRU-05's SourcedCents and
  TRU-06's SourcedDraft and keeps the stricter rule of each: signed amounts (an overdraft
  written without its minus sign is refused), dates, "*_cents" fields, thousands
  separators, and refusal of dollars, decimals and fractions of a cent. A nil Provenance
  or an empty run id fails closed.
- agent.HandlerSet resolves per (feature, tool): WithFeatureWrap adds a wrap for one
  feature, and the built tool picks per call by agent.FeatureOf(ctx). boot.AgentHandlers
  lists the wraps explicitly (trust.SourcedFeatures; no init()), and boot.ForgetRunner puts
  the run's feature in ctx. An agent call with no feature fails with agent.ErrNoFeature.
- Files outside a task list: app/legal/agent/handlerset.go, agenttest/feature.go (Fire
  scopes the feature), boot/agent.go. Non-trust drafts keep the default handler (BIL-02/05
  policy unchanged).

## Q-FEAT-BIL-03 F-BIL-03 trust application: trigger event, payee and gate kind

- No bill-delivered event exists on main. F-BIL-03 names `bill.delivered`
  (`billing.EventBillDelivered`); billing must publish it when delivery evidence sets a bill's
  delivered date, or the event half never fires. "Retainer permits" is checked by the run
  (the prompt), not by the binding. The `trust-to-fees` screen maps to subject `matter` with
  capability `trust.request`.
- The trust withdrawal module opens `billing.trust_fee_withdrawal` (G-B4) for a request with a
  `bill_id` (`withdraw.RequestGateKind`), so the Def declares that kind (Q-AGENT-002 reading A
  for the tool, B for the gate). `agent.WriteTools` still names `trust.withdrawal` as its spec
  kind; the tests assert the module's kind.
- By-Law 9: a fee transfer pays the firm's general account. Decided in BIL-FIX: the fixtures
  pay `agenttest.FirmPayee` and the Seed has a fee-bills port (`Seed.FeeBills`).
- The end-to-end tests use `app/legal/billing/ai_bilc_helpers_test.go` (prefix `bilc`), shared
  with FEAT-BIL-07 and FEAT-BIL-08. It supplies the bill, trust-balance and priced-time ports
  the Seed lacks; since BIL-FIX it builds over boot.AgentHandlers() and the driver and Seed
  share one Provenance.

## Q-FEAT-BIL-08 F-BIL-08 draft credit note: amount provenance on draft_document

- `draft_document` takes free-text markdown, so a credit note's amounts carry no provenance
  pointer and the app's handler checks none. F-BIL-08 adds `billing.CreditNoteDraftHandler`:
  `draft_document` with `billing.CheckCreditNote` ahead of it, which refuses
  (`agenttools.ErrUnsourcedValue`) any money figure in the body that no read tool returned in
  the run, before a draft row or a gate exists.
- Decided in BIL-FIX: boot.AgentHandlers wraps F-BIL-08's `draft_document` with
  `billing.CreditNoteWrap` (TRUST-DRAFT-WRAP's per-feature mechanism), replacing
  `CreditNoteDraftHandler`; the boot guard test covers it.
- The agent never computes tax: a partial credit states its pre-tax amount and the billing
  tax engine applies the place of supply when the note is issued, so an agent-computed tax
  figure is refused as unsourced.
- The `credit-notes` screen maps to subject `matter` with capability `documents.edit` (what
  `draft_document` needs).

## Q-FEAT-BIL-07 F-BIL-07 monthly billing digest: acting user and list_unbilled provenance

- Decided in BIL-FIX: the monthly run acts for the firm's designated lawyer
  (`trigger.BindDesignated`, JOB-ACTOR), never for a rule admin.
- The digest answers as `{answer, citations}` and passes `run.CheckAnswer`: it cites bill
  lines (`agent.BillLineCitation`) the run's get_bill returned. Its money figures are checked
  against the run's figures in the tests.
- Gap: `list_unbilled` is not wrapped with `Provenance.ReadTool` (get_bill and
  get_trust_balance are), so its amounts never enter a run's Provenance in production and a
  write citing an unbilled amount is refused as unsourced. Covered by PROV-UNBILLED and, since
  BIL-FIX, by boot.AgentHandlers, which records every read tool.

## BIL-FIX fee-transfer payee, credit-note wrap, digest actor, production handlers (decided)

- Decided as counsel. `prepare_trust_withdrawal` refuses a fee transfer for a bill
  (`purpose_category: fees` with a `bill_id`) to any payee but the firm's general account
  (`agent.ErrFeesNeedFirmPayee`, By-Law 9 s.9(1)). The F-TRU-02 fixtures had paid the matter
  contact for a bill and now pay `agenttest.FirmPayee`; their driver reads one delivered bill
  (`Driver.FeeBills`). F-BIL-03 shows the firm payee passing and a contact payee refused.
- boot.AgentHandlers now records every read tool into the run's Provenance itself
  (`recordedReads`), so a test over the production set sees what production does; the
  TRU-05 and bilc helpers no longer record reads themselves. AgentTools keeps that behaviour.
- agenttest: a FeatureDriver's Seed shares the driver's Provenance (production builds read
  and write tools over one), and the billing feature tests move to package billing_test so
  they can import boot.
- Files outside a task list: app/legal/agent/tools_write.go, agent/agenttest,
  boot/agent.go, trust/ai_f_tru_02_test.go and testdata/ai/f-tru-02, testdata/ai/f-bil-08
  (prompt hash after main's tool schema change).

## Q-FEAT-INT-02 F-INT-01 triage new lead: write path and trigger

- The trigger stays as FEAT-INT-01 bound it: `lead.created` only, since a received submission
  creates its lead. Note that intake does not publish `lead.created` yet; a portal submission
  publishes `identity.portal_submission` (EVENTS-WIRE-2/3), which starts F-CON-07. The
  EVENTS-WIRE follow-up should publish `lead.created` for every new lead, or F-INT-01 never
  starts.
- Lead passages are not filed documents, so the triage card keeps its citations (version,
  page, lines) in the draft body rather than in draft_document's `citations`, whose a2 schema
  needs a document uuid and a quote hash.
- The end-to-end tests use `app/legal/intake/ai_intb_helpers_test.go` (prefix `intb`), shared
  byte-for-byte with FEAT-INT-03 and FEAT-INT-04.

## Q-FEAT-INT-03 F-INT-02 intake mailbox watcher: event, gate kind and boot wiring

- "mail.received on the intake address" binds `email.received` (`intake.EventEmailReceived`),
  the same event F-DOC-08 proposes (`docs.EventEmailReceived`, Q-FEAT-DOC-09). No mailbox
  connector publishes it yet, and the event carries no recipient address, so the prompt tells
  the run to draft nothing for mail not sent to the intake address.
- The Def names `intake.lead_decision` as a literal: FEAT-INT-01 (in flight) owns the F-INT-01
  gate constant, and a second exported constant would clash at merge.
- Registering the Def in featureset makes boot's TestJobBindings and
  TestActionScreensCoverFeatures need it, so this task also adds its binding to
  `boot.JobBindings` and the `phone-and-email-capture` screen (subject `intake.lead`,
  capability `intake.leads`) to `actionScreens`.
  byte-for-byte with FEAT-INT-02 and FEAT-INT-04. F-INT-02 is an extraction Def, so it gets no
  plugin skill.

## Q-FEAT-INT-04 F-INT-03 missing-info chaser: event, gate kind and boot wiring

- "lead awaiting info" binds `intake.lead_awaiting_info` (`intake.EventLeadAwaitingInfo`,
  proposed; named like `intake.lead_ready_to_open`). a2 lists no such event and intake does
  not publish it yet (EVENTS-WIRE follow-up: publish it when a lead is set to wait on the
  prospective client). "schedule" is daily, bound with trigger.BindDesignated, so scheduled
  runs act for the designated lawyer in firm settings.
- The Def names `platform.skill_autonomy` as a literal; no Go constant for it exists yet.
  `boot.JobBindings` and the `lead-detail` screen (subject `intake.lead`, capability
  `intake.leads`) to `actionScreens`.
  byte-for-byte with FEAT-INT-02 and FEAT-INT-03.

## Q-FEAT-INT-06 F-INT-06 drafts its consult note: draft_document added to the Def and spec

- FEAT-INT-06 asks for gate intake.consult_outcome (G3) pending after a run whose first write
  is draft_note. draft_note always opens matter.agent_note (G-NT) on its own note, and that
  note is not a run draft, so submit_for_approval cannot open G3 on it. A consultation is on a
  lead, not yet a matter, so G-NT is not the outcome gate either.
- As decided for F-DL-02 (Q-FEAT-DL-04) and F-DOC-01 (Q-FEAT-DOC-02), draft_document is added
  to F-INT-06 in spec/ai/features.json and in ConsultNoteDef, and intake.consult_outcome is
  listed as a tool-gap kind (F-INT-06 tool_gap and tool_gaps.kinds; the kind count in
  kernel/agenttools/scope_test.go goes from 38 to 39). The run drafts the note
  with draft_document and opens G3 with submit_for_approval. draft_note stays in the Def as
  the spec lists it; the prompt tells the model not to use it. F-INT-15 has the same shape.
- Open: draft_note opens matter.agent_note through the plain GateOpener, not
  agenttools.ScopedOpener, so an agent run whose feature does not declare matter.agent_note
  (F-INT-06, F-INT-15, F-PLT-04) can still open it (ARC-I-035). Wrapping it in ScopedOpener
  is a one-line change to app/legal/agent/tools_draft_note.go, left to its owner.
- Events: consultation.ended and consultation.dictation_uploaded. The consult-outcome screen
  selects the consultation and needs intake.convert, the G3 approver capability.

## Q-FEAT-INT-08 F-INT-10 AI receptionist: event, gate kind const and boot wiring

- No event for an inbound call or chat exists on main or a pushed branch. F-INT-10 binds
  `receptionist_conversation.started`, named after a2's `intake.receptionist_conversation`
  row it starts with (intake.EventReceptionistConversationStarted).
- `intake.lead_decision` is written as a literal in the Def: FEAT-INT-01 and FEAT-INT-05 both
  add `intake.GateLeadDecision` on unmerged branches, so a third copy is not declared here.
  Once one lands, the Def can use it.
- Boot must bind every job feature and map every action screen (TestJobBindings,
  TestActionScreensCoverFeatures), so `app/legal/boot/agent.go` gains the F-INT-10 binding and
  the `ai-receptionist` screen (subject `intake.lead`, capability `intake.leads`, as the other
  intake screens).

## Q-FEAT-INT-09 F-INT-12 draft form from description: screen and draft target

- Boot maps every action screen (TestActionScreensCoverFeatures), so `app/legal/boot/agent.go`
  gains `intake-form-builder` (subject `intake.intake_form`, capability `intake.forms`).
- draft_document needs a matter_id and a form has none; the prompt has the agent draft on the
  matter the screen context names. A form-draft tool would remove that indirection.
- `intake.GatePublicWording` is new: no branch on main or pushed declares `intake.public_wording`.

## Q-EVENTS-WIRE-2 Routes for the evidence write points, draft subject_ref, wall and intake/party publishers

- Additive openapi paths (kernel/contract/additive.txt): `POST /deadlines/{deadline_id}/trigger`
  (PGBook.ChangeTrigger, `deadlines.edit`), `POST /filings/{filing_id}/issue` (IssueClaim),
  `POST /filings/affidavits-of-service` (RecordAffidavitOfService) and
  `POST /emails/{comm_item_id}/filings` (FileEmail), the last three on `documents.edit`. Each
  POST needs Idempotency-Key and answers problem+json. Module placeholders answer 503
  (`deadlines.Register` joins app/legal/modules.go; comms adds `registerFilingRoutes`); boot
  mounts the wired handlers from `HTTPPorts.Events`, built on the kernel DB and emitter.
  The trigger and issue routes check the capability firm-wide: the matter is only known after
  the store reads the row. The affidavit and email routes also check the body's matter, so a
  walled matter is 404.
- `draft_document` gains optional `subject_ref` (string, max 200) in spec/eng/a2-interfaces.json
  and agent/schemas/tools.json; it is kept in the draft's scope JSON. The F-CON-03 prompt sets it
  to the conflict run ref.
- `matter.PGWallStore` implements roles.WallStore and roles.WalledStore on matter.ethical_wall*;
  boot builds `roles.NewWallResolver(...).WithEvents(...)` (RestServices.Walls). The kernel
  emitter needs a tx and the wall check has none, so boot wraps it in `ownTxEmitter`, which
  opens its own tx when given nil.
- `party.organization_client_created`: party.Store.Create publishes it in the insert tx when
  the contact is an organization client (Store.Events, set at boot). `identity.portal_submission`:
  intake.Leads.Submit publishes it once per new lead (Leads.Events; a repeated key publishes
  nothing). Leads has no tx, so boot hands it the own-tx emitter (RestServices.LeadEvents); boot
  does not build Leads yet (no Postgres FormStore/LeadStore).
- Outside the listed files: app/legal/register_test.go (module count 36 -> 37 for deadlines),
  the F-CON-03 ok fixture prompt_hash and the regenerated plugin skill (prompt change).

## Q-FORM-SUBJECT F-INT-12 drafts public form wording on the firm subject

Decided as counsel: an intake form is firm-level, so F-INT-12 must not borrow a matter.
- draft_document accepts a firm draft: no `matter_id` and `subject_ref` `"firm"`
  (`agent.SubjectFirm`). It is stored only when the acting user holds `intake.forms`
  (`agent.CapIntakeForms`) and, for an agent run, the run's grant names it; else
  `ErrForbidden`. A firm subject with a matter, or no matter without the firm subject, is
  `ErrInvalidInput`. F-INT-12's Def grant gains `intake.forms`.
- Contract change, loosening only: `matter_id` is no longer in draft_document's `required`
  (`spec/eng/a2-interfaces.json`, `app/legal/agent/schemas/tools.json`); the handler still
  requires it for every draft that is not a firm draft.
- Migration `5802_draft_firm_subject.sql` drops NOT NULL on `docs.draft.matter_id`.
  `PGDocumentDrafts` stores a firm draft with a null matter, and the submit lookup returns
  `""` for it, so submit_for_approval checks no wall.
- Merged `task/EVENTS-WIRE-2` (the subject_ref field) into this branch, which is based on
  `task/FEAT-INT-09`. The register module count is 39 after the merge.
- Loosening the draft_document schema changes the prompt hash of every tool-use Def that
  offers it; the pinned `ok.yaml` prompt hashes of F-CON-03 and F-CON-04 are rehashed.

## Q-FEAT-INT-10 F-INT-14 engagement letter draft: gate kind and screen

- F-INT-14 reuses `docs.GateEngagementLetter` (G5, F-DOC-02): intake hands the letter to the
  engagement area, which owns the kind.
  gains `consult-outcome` (subject `intake.lead`, capability `intake.convert`).

## Q-INTEG-17B-INTAKE pass 17b intake integration choices

- One gate const per kind: `intake.GateLeadDecision` lives in ai_f_int_01.go; the copies
  FEAT-INT-05 (F-INT-04) and later branches brought are removed.
- Intake screens select the lead, `intake.SubjectLead` ("intake.lead"). FEAT-INT-05/06/07
  mapped `lead-conversion` to "lead" and `consult-outcome` to "consultation", neither a
  registered subject; both now use "intake.lead" like the other intake screens. Open: if
  the consult module gains its own subject, `consult-outcome` should move to it.
- Screen keys added by two branches (`consult-outcome`, `ai-receptionist`,
  `intake-automations`) keep one entry.

## Q-FEAT-INT-11 F-INT-15 non-engagement letter: draft_note cannot open intake.consult_outcome (decided)

The spec has F-INT-15 (and F-INT-06) open `intake.consult_outcome` (G3) through `draft_note`,
with no tool gap. The real `draft_note` handler cannot do that:

- It always opens `matter.agent_note` (`agent.GateAgentNote`), whatever the feature declares.
- It opens through the raw `GateOpener`, not `agenttools.ScopedOpener`, so an agent run opens
  `matter.agent_note` even when its run scope does not declare it (every other gate-opening
  write tool is scoped; ARC-I-035). In the F-INT-15 run the note draft is also stored before
  the gate open fails.
- `draft_note` returns a gate, not a draft, so `submit_for_approval` cannot open G3 on it.
- The G3 payload schema is a lead decision (`lead_id`, `outcome`, optional `limitation_date`),
  not a note (`matter_id`, `body_markdown`), so opening G3 with the note payload would fail the
  real gate kind.

Proposal: (a) wrap `draft_note`'s opener in `ScopedOpener` (a fix, independent of F-INT-15);
(b) decide how a consult letter reaches G3: either draft_note returns a draft (outcome
`draft`) when the run scope does not declare `matter.agent_note`, so `submit_for_approval`
opens G3 with a lead-decision payload, or add `intake.consult_outcome` to `tool_gaps` and give
F-INT-06/15 `draft_document`. Until then this branch ships the Def, prompt and the spec, prompt
and screen-action tests; the FromFeature and injection tests are written in full and skip on
this question. The Def is not added to featureset or the boot screen map, so no live route
runs a feature that cannot finish; add `intake.NonEngagementLetterDef()` there and the
`consult-outcome` screen (subject `intake.lead`, capability `intake.leads`) once answered.

Decided (INT-11-FIX): a non-engagement letter is a risk-management record, so it goes to G3
the same way as F-INT-06 (Q-FEAT-INT-06) and F-DL-02 (Q-FEAT-DL-04):

- draft_document is added to F-INT-15 in spec/ai/features.json and in
  NonEngagementLetterDef (Grant `documents.edit`), and F-INT-15 lists `intake.consult_outcome`
  as its tool gap (the kind is already in tool_gaps.kinds from Q-FEAT-INT-06). The run drafts
  the letter with draft_document and opens G3 with submit_for_approval; draft_note stays in the
  Def as the spec lists it and the prompt tells the model not to use it.
- The G3 payload schema (app/legal/gates/kinds.json and spec/gates.json) gains an optional
  `draft_id` (uuid), additively: the payload is `{lead_id, outcome, draft_id}`, where
  `draft_id` names the drafted letter or note the lawyer approves. Existing payloads stay valid.
- The FromFeature and injection tests run unskipped. The Def is in featureset; the
  consult-outcome screen is the one F-INT-06 registered in boot.
- The draft_note scoped-opener fix is SCOPE-FIX's.

## Q-FEAT-INT-12 F-INT-17 draft sequence: screen subject and shared helpers

- `intake-automations` is a firm-level settings screen; with no firm subject kind on main
  (BOOT-24 adds `boot.FirmSubject`, unmerged), it maps to subject `matter` (the matter the
  draft is filed on, which `draft_document` needs) with capability `intake.forms`. Move it to
  `FirmSubject` when BOOT-24 lands.
- `platform.skill_autonomy` is a literal in the Def: no exported constant exists on main or the
  pushed branches. The end-to-end tests use `app/legal/intake/ai_inte_helpers_test.go` (prefix
  `inte`), shared byte-for-byte with FEAT-INT-11 and FEAT-INT-13.

## Q-FEAT-INT-13 F-INT-19 receptionist criteria: screen subject and shared helpers

- `ai-receptionist` is a firm-level settings screen; with no firm subject kind on main
  draft is filed on, which `draft_document` needs) with capability `intake.forms` (a kernel
  `platform.*` capability is refused in app wiring by TestAppIDsAvoidKernelPrefix). Move it to `FirmSubject` when BOOT-24 lands.
- The firm website text reaches the run only as the quoted request; there is no web-fetch tool
  in the Def, so the prompt drafts only from what was quoted.
- `platform.skill_autonomy` is a literal in the Def. The end-to-end tests use
  `app/legal/intake/ai_inte_helpers_test.go` (prefix `inte`), shared byte-for-byte with
  FEAT-INT-11 and FEAT-INT-12.

## Q-FEAT-INT-16 F-INT-07 pipeline follow-ups: acting user and boot wiring

- The hourly run acts for the feature's designated lawyer (`trigger.BindDesignated`,
  JOB-ACTOR), never RuleAdmin; with no lawyer named it does not start.
- search_contacts and search_matters return no document citations, so the digest cites the
  contact records search_contacts returned (`contact:<id>`, page 1, line 1), which
  `run.CheckAnswer` accepts as citations of this run.
- Registering the Def in featureset makes boot's TestJobBindings need it, so this task adds
  its binding to `boot.JobBindings`. The end-to-end tests use
  `app/legal/intake/ai_intg_helpers_test.go` (prefix `intg`), shared with FEAT-INT-17 and
  FEAT-INT-18. F-INT-07 is a chat Def, so it gets no plugin skill.

## Q-FEAT-INT-17 F-INT-09 weekly intake digest: schedule and screen

- "Monday 08:00 America/Toronto" binds the weekly cadence (the first business day of the
  week; `trigger.ParseCadence` reads "monday"). The tick has no hour of day, so the run starts
  at the first tick of that day. The run acts for the feature's designated lawyer
  (`trigger.BindDesignated`, JOB-ACTOR).
- The `intake-dashboard` screen is added to `actionScreens` with subject `intake.lead` and
  capability `intake.leads`, the same as the other intake screens. The digest is firm-wide;
  if firm-level screens move to a firm subject (as decided for conflict-search-settings), this
  entry moves with them.

## Q-FEAT-INT-18 F-INT-20 map answers to lead: output shape

- F-INT-20 is an extraction Def with no OutputSchema: the screen-action route runs a
  non-tool-use feature inline and checks its answer against `run.AnswerSchema` with
  `run.CheckAnswer`, so the ranked rows and flag rows travel as the answer text, each row
  cited to a contact record the run's search_contacts returned. A per-row extraction schema
  would need the route to accept it (`run.Extract`); not done here.
- The `intake-questionnaire-send` screen is added to `actionScreens` with subject
  `intake.lead` and capability `intake.leads`. F-INT-20 is an extraction Def, so it gets no
  rule admin; open: switch CourtFormCurrencyBinding to BindDesignated. F-MAT-12 had no
  binding; boot binds it to document.uploaded and email.filed for the event's user.

## Q-FEAT-WRK-01 F-WRK-01 events, the work-item screen and create_task's due date

- Task: FEAT-WRK-01 (F-WRK-01 Pick up agent-ready items).
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (the Def in
  appDefs), `app/legal/boot/agent.go` (the job binding and the `work-item` screen),
  `app/legal/work/ai_wrk_helpers_test.go` (shared wrk test helpers) and the generated skill
  `paploo/counsel-plugin/skills/f-wrk-01-pick-up-agent-ready-items/SKILL.md`.
- No work event exists in spec/eng/a2-interfaces.json for "item enters Agent-ready" or
  "assigned to agent". The Def binds `work.item_agent_ready` and `work.item_assigned_to_agent`
  (consts in app/legal/work); nothing publishes them yet. Confirm the names when kernel/work
  publishes item events.
- The `work-item` action screen selects subject kind `work_item` with `work.edit`; no
  registry constant names that kind yet.
- `create_task`'s `due_on` is not held to the run's provenance in production (only
  propose_deadline, draft_bill and payment allocation use `SourcedWrite`). The wrk tests wrap
  create_task with `SourcedWrite("/due_on")` and check an unread date is refused with
  `ErrUnsourcedValue`. Proposed: wrap create_task the same way in
  `app/legal/agent/tools_task.go`. Note that none of the F-WRK tools returns a bare
  `YYYY-MM-DD` value (list_work_items gives RFC 3339 `due_at`), so once wrapped an agent can
  write no due date until a read tool returns one.

## Q-FEAT-WRK-07 F-WRK-11 base branch, the work-item-create screen and its skill

- Task: FEAT-WRK-07 (F-WRK-11 Create from email / call note).
- Based on `task/FEAT-WRK-01` (not yet on main) to reuse `work.GateWorkOutput` and the wrk
  test helpers; `origin/main` is merged in.
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (the Def in
  appDefs), `app/legal/boot/agent.go` (the `work-item-create` action screen) and
  `app/legal/work/ai_wrkc_helpers_test.go` (shared wrkc test helpers).
- `work-item-create` selects subject kind `matter` with `work.edit`: the drafted items go on
  that matter. F-WRK-11 is an extraction feature, so no plugin skill is generated for it.

## FEAT-TIM-01 Rebuild day: per-user end-of-day run and sourced time entries

- F-TIM-01 runs "end of day per user", but a `trigger.Binding` acts for one user. Boot binds
  the end-of-day run to the rule admin (`scheduled(tk.RebuildDayBinding)`) and the my-time,
  suggested-entries and activity-ledger actions (matter subject, `time.record`) cover each
  user's own runs. Open: a per-user schedule (one slot per timekeeper) in agent/trigger.
- `propose_time_entries` is not a `Provenance.SourcedWrite` in production, so an invented
  work date or minute count is not refused at boot. The F-TIM-01 feature test wraps it with
  `SourcedWrite("/entries/*/work_date", "/entries/*/minutes")` and checks ErrUnsourcedValue.
  Proposed (FEAT-TOOL-14 owner): declare those pointers in `buildProposeTimeEntries`; F-MAT-07
  reads its minutes from quoted transcript text, so it would need a structured source first.
- `app/legal/agent` imports `app/legal/timekeeping`, so the Def spells out its tool names and
  the gate kind (`agent.GateTimeAccept`) as strings.

## TIME-FIX Sourced time entries in production and per-user end-of-day runs

Done (decision TIME-FIX; LSO RPC 3.6-1: fees fair and accurate). Resolves the two open items
of FEAT-TIM-01 above.

- `buildProposeTimeEntries` now wraps the tool in `Provenance.SourcedWrite("/entries/*/work_date",
  "/entries/*/minutes")`; it reads `*agenttools.Provenance` from Deps. `Provenance.Observe`
  also records the dates and numbers written in the "text" strings of an untrusted envelope
  (a Quoted value, a read_document page line), so F-MAT-07 minutes from transcript quotes
  stay sourced. Values only are kept, never the text.
- `boot.AgentTools` wraps every handler read tool with the shared `Provenance.ReadTool`, so
  production read results record their values (before, only get_bill and the trust balance
  tool did).
- `trigger.BindPerUser` and `Bindings.WithUsers(ActiveUsers)`: each slot enqueues one run per
  active user, acting for that user, unique by `RunArgs.ForUser` (new, additive). A run whose
  ForUser differs from ActingFor is refused (`ErrCrossUser`). F-TIM-01 uses it instead of the
  rule admin. With no user source the binding is unready (`no_user_source`).
- Open: wire `AgentWiring.Timekeepers` to a store query of active timekeepers (users holding
  `time.record`, not disabled); until then F-TIM-01's end-of-day run does not start and the
  screen actions cover on-request runs.

## Q-FEAT-TIM-02 F-TIM-02 receipt capture: trigger events and shared helpers

- No receipt event exists. "receipt email or upload" binds the existing `document.uploaded`
  and `email.filed` events (the strings the documents and comms modules publish); the prompt
  has the agent draft nothing unless the file is a receipt. A dedicated receipt event, if one
  is added, replaces them.
- The `expenses` screen maps to subject `matter` with capability `time.record`.
- `draft_document` takes free-text markdown, so the expense amounts carry no provenance
  pointer; the prompt has the agent copy only values on the receipt or in a tool result.
- The end-to-end tests use `app/legal/timekeeping/ai_timb_helpers_test.go` (prefix `timb`),
  shared with FEAT-TIM-03 and FEAT-TIM-04. It prices the Seed's time entries for
  `list_unbilled` (the Seed has no `agent.MatterTime`).

## Q-FEAT-TIM-03 F-TIM-03 pre-bill check: trigger event and acting user

- No bill-draft-requested event exists on main. F-TIM-03 names `bill.draft_requested`
  (`timekeeping.EventBillDraftRequested`); billing must publish it when a user asks for a
  draft bill, or the event half never fires. The monthly half runs today.
- Decided in BIL-FIX: the monthly run acts for the firm's designated lawyer
  (`trigger.BindDesignated`, JOB-ACTOR), never for a rule admin.
- `matter-wip` and `write-downs` map to subject `matter` with capability `billing.view`
  (what `list_unbilled` needs).

## Q-FEAT-TIM-04 F-TIM-04 timesheet pre-review: trigger event

- No week-submitted event exists on main. F-TIM-04 names `timesheet.week_submitted`
  (`timekeeping.EventTimesheetWeekSubmitted`); timekeeping must publish it when a week is
  submitted for G9c, or the feature never fires. It has no screen action (spec `on_request`
  is empty).
- F-TIM-03's monthly run and F-DOC-09's daily run bind with `trigger.BindDesignated`: they act
  for the feature's designated lawyer in firm settings, never the rule admin. Their Binding
  funcs take no acting user; with no designated lawyer the feature is unready.

## Q-FEAT-TIM-05 F-TIM-05 trigger event, screen subject and value provenance

- No module publishes an engagement-letter-signed event yet; F-TIM-05 binds
  `engagement_letter.signed` (timekeeping.EventEngagementLetterSigned). The documents module
  publishes it when a letter reaches status signed.
- The matter-fee-arrangement screen selects the matter (a fee arrangement belongs to one
  matter), capability billing.view, in boot actionScreens.
- draft_document declares no provenance fields on main, so the rates and amounts in the
  drafted arrangement are not checked against the run's reads (ErrUnsourcedValue does not
  apply). The prompt forbids invented amounts and a lawyer approves G9e before the
  arrangement prices any time.
- The Seed has no time ports: the feature tests add a timekeeping fake and an
  agent.MatterTime over it (ai_timc_helpers_test.go), so list_unbilled runs its real handler.

## Q-FEAT-TIM-06 F-TIM-06 trigger event

- No module publishes a rate-change-drafted event yet; F-TIM-06 binds `rate_change.drafted`
  (timekeeping.EventRateChangeDrafted). The rates module publishes it when a rate change is
  drafted, before its effective date.
- The letters go through docs.outgoing (G8), reusing docs.GateOutgoing. draft_document checks
  no provenance on main, so the rates in a letter are not checked against the run's reads; the
  prompt forbids invented values and a lawyer approves each letter before it is sent.

## Q-FEAT-TIM-07 F-TIM-07 screen subject and value provenance

- The time-entry-detail screen maps to subject kind matter with time.record in boot
  actionScreens: no time-entry subject kind is registered, and the timekeeping HTTP routes
  check access on the entry's matter as well.
- propose_time_entries declares no provenance fields on main. F-TIM-07's prompt keeps the
  selected entry's work date and minutes, and the feature test checks the suggestion carries
  exactly the values list_unbilled returned. Wrapping the tool with
  Provenance.SourcedWrite("/entries/*/minutes", "/entries/*/work_date") would break F-TIM-01,
  whose proposals come from activity signals, so it is left to a tool-level decision.
- The gate kind is spelled "timekeeping.accept" in the Def (as in F-TIM-01) because
  app/legal/agent, which owns GateTimeAccept, imports this package.

## Q-FEAT-TIM-08-SCREENS Timekeeping screen actions select matters

- main has no `boot.FirmSubject`, so the timekeeping screens that are not about one matter
  (activity-list, and for FEAT-TIM-09/10 utilization, activity-categories and
  timekeeper-targets) select the matters of the rows on screen: `SubjectKind: "matter"` in
  `app/legal/boot/agent.go` actionScreens. matter-fee-arrangement is a matter screen.
- Capabilities: activity-list `time.record`, matter-fee-arrangement `billing.view`,
  utilization and timekeeper-targets `reports.firm`, activity-categories `time.view_all`.
  Switch the firm-level ones to the firm subject once boot has one.
- The features' end-to-end tests give list_unbilled a test MatterTime port over the Seed's
  time entries (the Seed has no MatterTime); the handler itself is the app's.

## Q-FEAT-WRK-01 F-WRK-01 events, the work-item screen and create_task's due date

- Task: FEAT-WRK-01 (F-WRK-01 Pick up agent-ready items).
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (the Def in
  appDefs), `app/legal/boot/agent.go` (the job binding and the `work-item` screen),
  `app/legal/work/ai_wrk_helpers_test.go` (shared wrk test helpers) and the generated skill
  `paploo/counsel-plugin/skills/f-wrk-01-pick-up-agent-ready-items/SKILL.md`.
- No work event exists in spec/eng/a2-interfaces.json for "item enters Agent-ready" or
  "assigned to agent". The Def binds `work.item_agent_ready` and `work.item_assigned_to_agent`
  (consts in app/legal/work); nothing publishes them yet. Confirm the names when kernel/work
  publishes item events.
- The `work-item` action screen selects subject kind `work_item` with `work.edit`; no
  registry constant names that kind yet.
- `create_task`'s `due_on` is not held to the run's provenance in production (only
  propose_deadline, draft_bill and payment allocation use `SourcedWrite`). The wrk tests wrap
  create_task with `SourcedWrite("/due_on")` and check an unread date is refused with
  `ErrUnsourcedValue`. Proposed: wrap create_task the same way in
  `app/legal/agent/tools_task.go`. Note that none of the F-WRK tools returns a bare
  `YYYY-MM-DD` value (list_work_items gives RFC 3339 `due_at`), so once wrapped an agent can
  write no due date until a read tool returns one.

## Q-FEAT-WRK-02 create_task returns a proposal, so F-WRK-02 cannot open work.plan in the run

- Task: FEAT-WRK-02 (F-WRK-02 Propose matter plan). Same gap as Q-FEAT-MAT-13 for work.plan.
- `agent.WorkItemGateKinds` holds `work.absence` and `work.output` only, so under F-WRK-02's
  scope (work.plan) `create_task` returns `OutcomeProposal`, the run records no draft and the
  in-run `submit_for_approval` for `work.plan` is refused with `ErrNotFound`: the run fails
  closed (tasks proposed, no gate). `TestFWRK02_FromFeature` pins that, then opens work.plan
  on the proposed plan as the acting user and checks pending, the agent principal 403 and
  `ErrSelfApproval` for the acting user.
- work.plan's payload schema is `{matter_id, template_id}` (a matter's plan, not one item), so
  adding it to `WorkItemGateKinds` does not fit. Proposed: let `submit_for_approval` open
  work.plan on the matter (`subject_type: matter`) when the run created proposals on it, and
  pass the template id the trigger carries (FEAT-TOOL-16 follow-up in
  `app/legal/agent/tools_task.go` / `tools_submit.go`).
- `matter.template_applied` is not in spec/eng/a2-interfaces.json; nothing publishes it yet.
  `matter.opened` is the a2 event.
- Files beyond the list: featureset.go and its test, boot/agent.go (the job binding), the
  shared `ai_wrk_helpers_test.go` and the generated skill
  `paploo/counsel-plugin/skills/f-wrk-02-propose-matter-plan/SKILL.md`.

## Q-FEAT-WRK-03 F-WRK-04 date shift: work.plan in the run, and no sourced shifted date

- Task: FEAT-WRK-03 (F-WRK-04 Date-shift on key date change).
- Same work.plan gap as Q-FEAT-WRK-02: the change tasks are proposals and the in-run
  `submit_for_approval` fails closed with `ErrNotFound`; `TestFWRK04_FromFeature` opens
  work.plan on the change as the acting user and checks pending, 403 and `ErrSelfApproval`.
- The Def's tools return no bare `YYYY-MM-DD` date (list_work_items gives RFC 3339 `due_at`)
  and there is no date-shift tool, so the agent cannot source a shifted due date. The prompt
  has it leave `due_on` out and let the lawyer set it; the tests check an invented shifted
  date is refused with `ErrUnsourcedValue` (with create_task wrapped as in Q-FEAT-WRK-01).
  Proposed: a read tool that returns each dependent item's shifted date (offset from the key
  date, in America/Toronto business days where the template says so), so a date-shift
  proposal can carry dates.
- `matter.key_date_changed` is not in spec/eng/a2-interfaces.json; nothing publishes it yet.
- Files beyond the list: featureset.go and its test, boot/agent.go (the job binding) and the
  generated skill `paploo/counsel-plugin/skills/f-wrk-04-date-shift-on-key-date-change/SKILL.md`.

## Q-FEAT-WRK-04 F-WRK-06 workflow lint: event, screen subject and G-W3 subject

- Task: FEAT-WRK-04 (F-WRK-06). New event `work.workflow_draft_saved` (work.EventWorkflowDraftSaved)
  has no publisher yet; add it to the next events task with the workflow editor's save.
- boot.FirmSubject is not on main, so the firm-level `workflow-editor` screen maps to the matter
  subject (capability work.workflows) until BOOT-24 merges; then switch it to FirmSubject.
- work.workflow_publish is a tool-gap kind: the run drafts the lint report with draft_document
  and opens G-W3 on that draft. The G-W3 payload schema names {workflow_id, version}; the gate
  payload is the draft. Open: carry workflow_id/version once the workflow editor exists.

## Q-FEAT-WRK-05 F-WRK-07 rule suggester: draft rule subject and PROPOSAL-DRAFT

- Task: FEAT-WRK-05 (F-WRK-07). work.automation is not a work-item gate kind
  (agent.WorkItemGateKinds), so create_task returns a proposal. The branch carries the
  PROPOSAL-DRAFT change to app/legal/agent/tools_task.go and its test (db1212d, byte-identical),
  which records a proposal as a run draft, so the run submits the proposed rule task for
  work.automation in the run. Merge after (or with) task/PROPOSAL-DRAFT; the files merge clean.
- The G-AR payload schema names {rule_id}; until work.automation_rule has a store the draft rule
  is a proposed work item. Open: create an automation_rule draft once the work-automations
  service exists.
- The weekly run acts for the designated lawyer (BindDesignated, JOB-ACTOR); the firm-level
  work-automations screen maps to matter until boot.FirmSubject lands (see Q-FEAT-WRK-04).

## Q-FEAT-WRK-06 F-WRK-09 absence cover planner: absence events and screens

- Task: FEAT-WRK-06 (F-WRK-09). New event `work.absence_requested` (work.EventAbsenceRequested)
  has no publisher yet; the approved event reuses deadlines.EventAbsenceApproved
  (`work.absence_approved`, F-DL-09). Add both publishers to the next events task with the
  team-availability service.
- The daily run and both events act for the designated lawyer (BindDesignated, JOB-ACTOR) and
  fail closed with none set.
- workload and team-availability are firm-level screens (capability work.edit); they map to
  matter until boot.FirmSubject lands (see Q-FEAT-WRK-04).
- FEAT-WRK-06 carries PROPOSAL-DRAFT's tools_task.go: create_task now records each agent
  proposal as a run draft, so F-WRK-02 and F-WRK-04's in-run submit_for_approval opens the
  work.plan gate instead of failing closed (Q-FEAT-WRK-02). Their tests now assert the one
  in-run gate on a proposed item, pending, prepared for the acting user, decided only by
  another lawyer.

## Q-FEAT-WRK-08 F-WRK-14 base branch and the workflow-templates screen

- Task: FEAT-WRK-08 (F-WRK-14 Draft template from past matters).
- Based on `task/FEAT-WRK-07` with `task/FEAT-WRK-04` merged in, to reuse
  `work.GateWorkflowPublish` and the wrkb draft_document test helpers (neither is on main yet).
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (the Def in
  appDefs), `app/legal/boot/agent.go` (the `workflow-templates` action screen) and the
  generated skill `paploo/counsel-plugin/skills/f-wrk-14-draft-template-from-past-matters/SKILL.md`.
- boot.FirmSubject is not on main, so the firm-level `workflow-templates` screen selects the
  completed matters to mine (subject `matter`, capability `work.workflows`); switch it to
  FirmSubject with `workflow-editor` once BOOT-24 merges.
- work.workflow_publish is a tool-gap kind: the run drafts the template with draft_document on
  the first selected matter and opens G-W3 on that draft. Open: carry a workflow template id
  in the G-W3 payload once workflow templates are stored.

## Q-FEAT-WRK-10 F-WRK-05 registration outside the task's files

- Task: FEAT-WRK-10 (F-WRK-05 Daily work digest).
  appDefs), `app/legal/boot/agent.go` (the job binding) and
  `app/legal/work/ai_wrkd_helpers_test.go` (the shared test helpers of the read-only work
  features).
- F-WRK-05 is scheduled, so it binds with `trigger.BindDesignated` and runs for the designated
  lawyer in firm settings; with none named readiness flags it (JOB-ACTOR).

## Q-FEAT-WRK-11 F-WRK-08 automation run events

- Task: FEAT-WRK-11 (F-WRK-08 Explain automation runs).
  appDefs) and `app/legal/boot/agent.go` (the job binding). Built on task/FEAT-WRK-10 for the
  shared read tools and `ai_wrkd_helpers_test.go`.
- No event exists in spec/eng/a2-interfaces.json or kernel/work for "automation run failed or
  skipped". The Def binds `work.automation_run_failed` and `work.automation_run_skipped`
  publishes automation run events.

## FEAT-WRK-13 Action screens of the read-only work answers

- F-WRK-15, F-WRK-16 and F-WRK-17 run from work-automations, work-categories and
  work-reports. Main has no boot.FirmSubject, so the action route's screen map selects a
  matter on each (work.view). Open: these are firm-level screens (an automation run, the
  category list, a report figure); switch them to the firm subject once it lands.
- Test helpers are one file per package: app/legal/intake/ai_int_helpers_test.go,
  app/legal/timekeeping/ai_tim_helpers_test.go and app/legal/work/ai_wrk_helpers_test.go
  hold every branch's helpers (prefixes int..inth, tim..timd, wrk..wrke kept, so names stay
  unique). Earlier entries naming ai_int<x>/ai_tim<x>/ai_wrk<x>_helpers_test.go refer to them.
  The timc helpers wrap read tools with the run's Provenance, as boot does since TIME-FIX.
- `work-automations` keeps FEAT-WRK-11's capability `work.workflows`; FEAT-WRK-13's read-only
  answers asked for `work.view` on the same key. Open: whether the read-only answers need a
  separate screen key.
- `ai-receptionist` keeps FEAT-INT-08's subject `intake.lead` / `intake.leads`; FEAT-INT-13
  (F-INT-19, firm settings) asked for `matter` / `intake.forms`. Open until a firm subject
  exists (BOOT-24).

## BOOT-24 / BOOT-25 (composition root)

- Files beyond the task list: `app/legal/boot/production.go` (new `Options.Production`: the
  Postgres graph is built at Register on `k.DB`, because kernel boot opens the pool inside
  `boot.Run`), `boot.go`, `routes.go` (matter reads, contacts and `/firm/tax-registrations`
  mounted), `agent.go` (every scheduled feature binds with `trigger.BindDesignated`; the
  `RuleAdmin` field is replaced by `Designated`, default `agent.PGDesignatedLawyers`;
  `conflict-search-settings` and `compliance-audit-log` use subject kind `firm`).
- Not wired yet, so `testdata/unwired_routes.golden` lists the routes that still answer 503:
  no request authentication exists (Production.Principal defaults to "nobody", so served
  routes answer 401); no Postgres `matter.Updater`, docs, work, time-entry, deadline lister,
  access directory or trust-request wiring; GatePorts (GL, Sealer, withdraw.Auditor, credit
  second approval) and AgentPorts have no Postgres composition. `HTTPPorts.AgentModel` and the
  HandlerSet are on `task/PG-RUNS-WEBHOOK`, which this branch could not merge.

## AUTH-WIRE: member sign-in at boot

- **Choice logged (spec default):** a2 ARC-I-017/-036 and the openapi `session` scheme name an
  OIDC session cookie (`counsel_session`); `core.sign_in_policy.idp_protocol` also allows
  `saml`. Implemented OIDC only (authorization code + PKCE, RS256 ID tokens) in
  `kernel/identity/login`, on the standard library because no OIDC module is on the AGENTS.md
  allow list. SAML, ES256 ID tokens and reading lifetime/idle/lockout from
  `core.sign_in_policy` at request time are not done; the DDL defaults (720/30 min) are used.
- **Session handle:** `core.user_session` has no token-hash column, so the cookie is an
  HMAC-signed `(session id, mfa time)` keyed by `$COUNSEL_SESSION_KEY` (>= 32 bytes, required
  outside dev). Every request re-reads the row, so logout, idle, expiry, revocation and
  offboarding end the session. No migration was added.
- **Step-up:** `GET /auth/login?step_up=1` asks the IdP for fresh auth (`max_age=0`,
  `prompt=login`, optional `acr_values`) and needs a second-factor `amr`. Production sets the
  new `HTTPPorts.StepUp func(*http.Request) time.Time` from `login.Service.StepUp` (zero = no
  MFA or no live session), which is exactly `gates.DecideHTTP.StepUp`. Mounting `DecideHTTP`
  still needs `Gates gates.Engine` (Decide/Withdraw/Get over the kernel engine), `Kinds
  *gates.Catalog` and `Now`; that mount is left to the gate-routes task.
- **Files outside a task list:** `kernel/server/routes.go` gains `(*Mux).MountRoot` so the
  browser redirects `/auth/*` live outside `/v1` and outside openapi conformance (not on the
  frozen `Server` interface); `app/legal/boot/production.go` gains `Production.Login`;
  `cmd/counseld/main.go` gains sign-in flags (secrets from env only) and `-env`/`-dev-login`.
- **Open:** sessions authenticate, but production `Roles`/`Assignments` are still empty
  (no Postgres `RoleAssignments`), so a signed-in member gets 403 on capability-checked
  routes until that port lands.

## PG-MATTER-UPDATER / PG-GATE-PORTS

- `matter.PGUpdater` writes the plain fields (short name, practice area, court and property
  province) at the If-Match version inside a tx bound to the principal (RLS walls apply);
  it also refuses a user screened by an active wall (404) and a stale version (412).
  `matter.PGWalls` reads active walls for the Lifecycle dry run; `AddWall` is refused
  (walls are gate-owned). Boot mounts `matter.Writes` (POST and PATCH /matters).
- `boot.gatePortsPG` composes the GatePorts from Postgres: `PGGL` (ledger.PG with posted_by
  read in the gate tx: the latest approver, else the session user), `trust.AESSealer`,
  `withdraw.AuditLog` on the kernel audit log, the signatory/signer/firm ports, the bill
  effects and the write-off ports; the managing lawyer's second approval holds
  `billing.writeoff_second_approve`. Gates with min_approvers >= 2 keep their quorum in the
  kernel engine.
- Open: `Production.SealKey` (and `BillPrefix`, default `INV`) has no config source yet:
  `cmd/counseld` passes `Production{}`, so production registers no money gate effects until
  the binary reads the seal key (env or mounted file). Files beyond the task list:
  `app/legal/boot/production.go`, `routes.go`, `compose_test.go`.
- Still unwired: `POST /gates/{gate_id}/decisions` and `/withdraw` need a step-up source
  (`gates.DecideHTTP.StepUp`), which waits on request authentication.

## BOOT-CONFIG: a signed-in member can use Counsel

- **Roles were never registered.** `roles.Register` had no registrar, so the production
  catalog was empty and every capability check failed (403, or 500 on unknown capability).
  `app/legal/roles` now embeds a byte-for-byte copy of `spec/legal/access.json`
  (`TestAccessDataMatchesSpec`) and registers it. Production fills its checker's catalog from
  the sealed registrations (`identity.Roles.Load`) and reads each member's role per check
  from `core.app_user.role_id` -> `core.role.key` (`identity.PGAssignments`; active user and
  active role only).
- **Choice logged (needs a spec answer):** the kernel checker is boolean, but access.json has
  levels, and many gate approver capabilities are also plain work capabilities
  (`matters.edit`, `admin.users`, `billing.payments`, ...). To keep ARC-I-018 (admin and agent
  never approve; viewing is not approving), a role holds a gate approver capability only at
  level `all` or `approve`, and admin and agent never hold one. Fail-closed side effects:
  associate/paralegal `own` on `matters.edit`, admin `all` on `admin.users`/`admin.roles`,
  auditor `view` on approver capabilities are not granted. Custom roles (`core.role_grant`,
  `based_on_id`) are not loaded: a member on a custom role gets 403.
- **Gate decisions:** `POST /gates/{gate_id}/decisions` and `/withdraw` are mounted on the
  kernel gate engine (it is the `gates.Engine` port as is; `HTTPPorts.Decisions` overrides
  it), the spec catalog and the kernel clock. Without a sign-in service, StepUp is "never",
  so every decision answers 401 step_up_required.
- **Seal key:** `counseld` and `counsel-worker` read `$COUNSEL_SEAL_KEY` (base64, 32 bytes)
  or `-seal-key-file`/`$COUNSEL_SEAL_KEY_FILE`; `-env`/`$COUNSEL_ENV` (worker gained `-env`).
  Outside dev, boot refuses to start without one (`ErrSealKeyMissing`); dev uses a random
  per-process key and warns without showing it. `boot.Secret` redacts in fmt and slog. The
  Helm chart does not mount the secret yet.
- **Session limits:** the session read joins `core.sign_in_policy` (singleton); its
  lifetime and idle limits win, and the config/DDL defaults apply only with no policy row.
  The cookie Max-Age still uses the config lifetime.

## Q-TRUST-SCOPE prepare_trust_withdrawal checks the run's gate kinds; boot wires its write deps

- `agent/handlers.go`: the handler calls `agenttools.CheckGateKind(trust.withdrawal)` before
  `WriteTools.Call`, so an undeclared kind is refused (ErrGateKindNotAllowed) before the trust
  module records anything (ARC-I-035). `gate_scope_test.go` adds it to the table test and a
  second AST guard: every `*WriteTools` `.Call` must follow a `CheckGateKind` call.
- `agent/tools_write.go`: `Call` also refuses (ErrMissingDep) when Can, Walls, TrustAccount or
  Withdrawals is nil, instead of panicking.
- `boot/agent.go` (additive): `AgentWiring.Write` and `AgentWriteTools` build the write tools when
  `Ports.WriteTools` is nil: Can, Walls, Provenance, MatterContacts and TrustAccounts from the
  ports; Withdrawals (wrapped to Request and Available only), FirmAccount and Bills from
  `Write`. Any nil port fails boot with agent.ErrMissingDep. A matter with zero or several
  trust ledgers gets no trust account (fail closed).
- Open: `Production` still does not build `AgentWiring`; the caller supplies `Write.Withdrawals`.

## Q-INTEG-17A-PARTY-SCOPE propose_conflict_parties accepts either G2 kind

- SCOPE-FIX made `propose_conflict_parties` check only `conflict.clearance`, but F-MAT-04,
  F-MAT-16 and F-MAT-18 (spec/ai/features.json) declare `matter.party_conflict`, the matters
  G2 their proposed parties wait on. The tool now accepts a run that declares either kind and
  still refuses one that declares neither (ErrGateKindNotAllowed, nothing recorded).
- `matterkind.GatePartyConflict` (additive) names the kind so the agent package need not
  import matter; `matter.GatePartyConflict` is that constant. `gate_scope_test.go` adds a row
  for the matter kind.

## BOOT-REST-3

- Merged task/BOOT-REST-2a; kept receipt_http.go's trust.TxRunner (2a had dropped it).
- Deadline reminders: `PGBook.List` is the Postgres `deadline.Lister`; `Reminders` skips
  met, released and superseded deadlines. `deadline.SentLog` now takes a typed `SentKey`
  (kind, deadline, user, day); `PGSentLog` stores it in `cal.deadline_reminder_sent`
  (1102, unique NULLS NOT DISTINCT), so a restart never resends a reminder that day. Boot
  (services_rest3.go) defaults Deadlines to the rest book, Sent to PGSentLog and, with
  `DeadlinePorts.Recipient`, Primary to the kernel notification service (templates
  deadline.reminder / deadline.digest, ids and dates only). The fallback notifier stays
  caller-supplied; reminders carry no per-user recipient yet (UserID empty).
- Webhooks: 1404 adds `platform.webhook.row_version` (default 1) and PGExpiry.SetExpiry bumps
  it. Open: no Postgres `webhook.EndpointReads` exists and register.go mounts `webhook.HTTP{}`
  without ports, so the routes still answer 503; the read must select row_version as
  EndpointInfo.Version and the renew must be conditional on it (412 on mismatch).
- Open: HTTP-25 RunService. PORT-18's PGRuns is the conflict-check run store
  (conflict.check_run), not agent.agent_run, so it cannot back agent runs; an agent run store
  is needed first. Routes still answer 503.
- Open: Postgres payment.Proposals (HTTP-15). billing.payment_allocation has no proposed
  state and no Postgres payment.Service exists, so the gated payment routes still need both
  (a proposal table in the billing block, and a Postgres Service for Get) before they leave 503.

### PG-PAYMENT

- Resolves the open HTTP-15 item above. 5630 (billing block) adds three tables, additive only:
  `billing.payment_allocation_proposal` (proposed sets; superseded, never deleted; a deferred
  trigger keeps the proposed total within the payment), and, because the frozen
  `billing.payment` has no column for them, `billing.payment_capture_key` (a manual capture's
  idempotency key) and `billing.payment_reversal` (the reversal reason).
- Question: `billing.payment.payer_name` is NOT NULL but Capture carries no name and billing may
  not read party tables (ARC-I-041), so PGService writes ''. The payer is `payer_contact_id`.
- Question: Reverse has no actor, so its -1 x rows carry the original `allocated_by`.

### PAYMENT-FIX

- Answers both PG-PAYMENT questions (decided as counsel).
- Contract change (payment/contract.go): `Service.Reverse` gains `by Actor`
  (`Actor{UserID, Agent}`), and `Payment` gains `ReversedBy`, with `ErrAgentReverse`. A
  reversal must record who reversed it for the books' audit trail. The only implementations are
  PGService and paymentfake, and nothing in production calls Reverse yet. PGService refuses
  an agent: either the Actor says Agent, or the user's `core.app_user.principal_kind` is not
  'human'. 5630 `billing.payment_reversal` gains `reversed_by` (NOT NULL, FK app_user). The
  -1 x allocation rows also carry the reverser as `allocated_by`, never the original allocator.
- payer_name: PGService gains `Payers PayerNames`, a port that billing defines. Boot wires it to
  the party module API (`boot.PartyPayers` over `party.Contacts.Get`, which gives the legal name). Billing
  never reads party tables. While the port is nil, or the payer has no name, Capture fails closed
  (`ErrNoPayerNames` / ErrInvalid) and nothing is written.
- Unapplied money (overpayment) is not a payment_allocation row; Allocations derives it as
  amount minus applied, as paymentfake does. Turning it into client credit stays G-B3's.

## PG-RUNS-WEBHOOK

- Webhooks: `webhook.PGEndpointReads` reads platform.webhook with `row_version` as
  EndpointInfo.Version (never secret_ref). PATCH keeps 428 (no If-Match) and 412 (stale), and
  with `PGExpiry.SetExpiryAt` (new `VersionedExpiry`) the renew is guarded by the version read,
  so a write between the check and the renew is 412 (`ErrStaleVersion`). Boot mounts the
  routes over the kernel DB (services_agent.go); `HTTPPorts.WebhookEndpoints` overrides the reads.
- Agent runs: agent.agent_run (0001) needs `goal text NOT NULL`, a uuid skill FK and other
  states, so 1901 adds `agent.run_record` (ids, feature, skill, acting-for, grant, subject,
  state, error code, timestamps, tokens, cost in micro-cents; no conversation column).
  `run.PGRecords` stores it; `agent.RunStarter` is the HTTP-25 RunService: a skill is a
  feature id in lower case, the run is driven in the background by the kernel run loop as the
  AgentPrincipal with the narrowed grant (the loop otherwise uses the feature's full grant).
  The outcome text is dropped. Boot builds it when `HTTPPorts.AgentModel` and `AgentTools`
  are set; RunAccess defaults to Access when it builds agent principals.
- Open: no composition root sets AgentModel/AgentTools yet, so a live server still needs
  them passed in; failed runs are not retried (agent_run_retry is not wired to run_record).

## Q-EVENTS-WIRE-6 lead.created, intake.lead_awaiting_info and email.received recipients

- **lead.created:** `intake.Leads.Submit` now publishes `lead.created` (`lead_id`) for every new
  lead, next to `identity.portal_submission`, so F-INT-01, F-CON-01 and F-DL-02 start. A repeat
  with the same idempotency key publishes nothing. Portal submission is the only lead-creating
  path today; any new one must call the same publisher.
- **intake.lead_awaiting_info (additive to spec/eng/a2-interfaces.json):** intake.json lists
  "awaiting info" as a lead state and the missing-info chaser triggers on it, but no event or
  status existed. Added `intake.LeadAwaitingInfo` (contract.go, additive constant), the
  `LeadStatuses` port with `PGLeads.SetStatus`, `Leads.SetStatus` and migration
  `5302_lead_status.sql` (additive `intake.lead.status`, default `open`). Moving into
  `awaiting_info` from another status publishes one `intake.lead_awaiting_info` (`lead_id`);
  staying there publishes nothing. Open: no HTTP route sets the status yet (a contract change
  to api/openapi.yaml); the emitter opens its own tx, as identity.portal_submission does.
- **email.received `to` (additive payload field):** the payload carries `to`, the
  lower-cased, de-duplicated to/cc/bcc addresses read from the participants array (entries with
  `role` to, cc or bcc and an `address` or `email`), never nil. This relaxes the earlier
  "never addresses" note for recipients only (the firm's own mailboxes in practice), so F-INT-02
  can ignore mail not sent to the intake address. The sender, subject and body stay out.

## ROLE-LEVELS (supersedes the BOOT-CONFIG role-level choice above)

- **Level model:** capability levels are the access.json names, ordered
  none < view < own < all < approve. `kernel/identity` gains additive `Roles.LevelOrder`,
  `Roles.DecideLevel`, `Roles.Grant` and `Checker.CanAt(ctx, p, capability, level, s)`; `Can`
  is `CanAt` at the weakest holding level. Boot loads the spec levels with
  `roles.ApplyLevels` after the registrations; every role registers each capability it holds
  above none (associate/paralegal edit matters, admin manages users, auditor views).
- **Deciding:** a check at `roles.DecideLevel` (all) or above on a gate approver capability
  is a decision. The gate decision route uses `CanAt(..., "all")` (`gates.LevelAccess`); the
  agent and admin never pass it; preparer != approver stays in the gate engine. Caveat: a
  future non-decision route that needs `all` scope on an approver capability would be read
  as a decision; such a route should check `own` and rely on the subject resolver.
- **Custom roles:** a role key the app did not register is read per check from
  `core.role_grant` (`PGAssignments.GrantsOf`, active roles only), capped at the strongest
  built-in level per capability. An unknown role holds nothing (403).
- **Helm:** the chart syncs `counsel-<cell>-seal-key` from the cell secret store, mounts it
  read-only and passes `-seal-key-file`. **Cookie:** Max-Age is what is left of the
  sign-in policy lifetime (config lifetime with no policy row).

## RECON-REFUSE: a stored reconciliation that disagrees with its items

- Answers the PG-WRITES-FIX open question (counsel decision): a stored row whose
  adjusted_bank_cents is not bank_balance_cents + the signed sum of its items is refused for
  sign-off and reported as `unbalanced`, never as stored (rule BIL-I-009, By-law 9 s.18(8)).
- Additive contract change in `app/legal/trust/recon/contract.go`: `ThreeWay.ItemsAgree()`.
  `Balanced()` now also requires it, so the read and write views report such a row as
  `balanced: false`. A computed comparison always agrees, so no live path changes.
- POST /trust/reconciliations/{id}/submit answers 422 with problem code `unbalanced`
  (`recon.CodeUnbalanced`) before the three-way check; the sign-off effect (`PGSignOff.Sign`)
  re-reads bank_balance_cents and outstanding_items under FOR UPDATE and returns ErrUnbalanced.

## PROPOSAL-DRAFT: proposals are drafts of the run (closes Q-FEAT-MAT-13)

- Counsel decision: `create_task` and `propose_conflict_parties` record their proposal (the
  work item, the conflict run) as a draft of the agent's run scope, so `submit_for_approval`
  can open the feature's gate on it in the run. The outcome is unchanged (a proposal, or a
  draft when the scope declares a work-item gate kind). The run's gate-kind check still
  applies, the gate is prepared by the acting user through the agent, and the agent never
  decides it. A user principal, or an agent outside its run scope, records nothing.
- `TestAI_R_003_CreateTaskDraft` now expects an agent's proposal in its own scope to be
  recorded. The F-MAT-16..18 FromFeature tests submit in the run and check the run's gate
  (every earlier assertion kept: pending, prepared for the acting user, app 403s, kernel
  ErrAgentApprover and ErrSelfApproval, still pending).
- The branch merges origin/task/FEAT-MAT-14 (which carries FEAT-MAT-13 and FEAT-MAT-15) so
  their tests could be updated; the integrator merges this branch instead of those three.
  It adds the `matter-open-wizard` (matter, matters.open) and `contact-detail` (contact,
  contacts.view) action screens that F-MAT-16..18 need (TestActionScreensCoverFeatures).
- agenttest: `Seed.Deps` resolves conflict runs (`conflict.SubjectRun`) and work items
  (`agenttest.SubjectWorkItem`) for submit_for_approval. Open (boot): `PGSubjectMatters` knows
  neither `conflict.run` nor `work_item`, so in production the submit fails closed with
  ErrSubjectKind until boot maps both to their matter.

## Q-PARTY-ADDED matter.party_added (additive a2 event)

- Counsel decision: a party added to a matter gets its own event, `matter.party_added`; it is
  not mapped onto `matter.updated`. Additive to `spec/eng/a2-interfaces.json` (module matter,
  subject matter, payload `matter_id`, `conflict_check_id`, `check_party_ids[]`, `added_by`;
  ids only, never a party's name). Constant `matter.EventPartyAdded`.
- Published where parties are added on main: `boot.PGConflictParties.ProposeParties`, when the
  conflict check belongs to a matter, once per call in the same transaction as the
  `conflict.check_party` rows (ARC-I-030). A check on a lead only publishes nothing. With no
  emitter a matter's check is refused (ErrMissingDep), like the other publishers.
- No other Go write path adds a `matter.party_role` row yet; when the matter module gains one,
  it publishes the same event.
- BOOT-24: call `boot.SetMatterPortEvents(&ports, k.Emitter)` after `WireMatterPorts`. F-MAT-10
  ("event: party added or matter opened") should bind `matter.party_added` and `matter.opened`.

## Q-TOOL-DATES shift_date added to the a2 tools catalogue; create_task due_on is sourced

- Additive a2 change: new read tool `shift_date` (capability `deadlines.view`, manifest scope
  `data.read`), appended to `spec/eng/a2-interfaces.json` and `app/legal/agent/schemas/tools.json`
  (`schemas.Count` 25 -> 26). Input: `date` (YYYY-MM-DD), `days` (integer, may be negative),
  `unit` (`calendar` | `business`). Output `{from, unit, date}`; the day count is not echoed so
  a model-chosen number never becomes a sourced value. For an agent the start `date` must
  itself be a date the run read (ARC-U-027), so shift_date cannot launder an invented date.
- Business days skip weekends and `rulepacks/ontario.Holidays()` (America/Toronto). That
  calendar is marked unverified behind `deadline.ontario_pack`; shift_date uses it
  unconditionally. Open: gate the holiday list on the flag (weekends only when off)?
- `create_task` `/due_on` is now a SourcedWrite. compute_deadline, list_deadlines,
  list_unbilled and list_work_items now record their results in the run's Provenance
  (PROV-DATES); list_work_items adds an additive `due_on` (business-zone day) next to the
  RFC 3339 `due_at`, which provenance cannot match. A date only inside quoted document text
  is not sourced: F-MAT-17's placeholder task no longer carries a due_on.
- `spec/eng/a2-interfaces.html` still lists 25 tools; regenerate it if it is generated.

## Q-INTEG-17A-BOOT pass 17a boot follow-ups

- `PGSubjectMatters` resolves `conflict.run` (the check run's check's matter) and `work_item`
  (the item's matter_id), the subjects PROPOSAL-DRAFT submits.
- `registerAgent` calls `SetMatterPortEvents` with the kernel emitter after the ports are
  built (WireMatterPorts), so PGConflictParties publishes matter.party_added. Open:
  `Production` still builds no `AgentWiring`, so nothing calls WireMatterPorts in counseld yet
  (AGENT-PROD).
- F-MAT-10 binds `matter.party_added` and `matter.opened` (no longer `matter.updated`).
- JobBindings takes no rule admin: F-CON-09/11/12 and F-DOC-09 now bind to the designated
  lawyer like every other scheduled feature.
- Open: PROPOSAL-DRAFT mapped `matter-open-wizard` to `matters.open`; main's pass-16 mapping
  (`matters.view`, Q-INTEG-16-SCREENS) was kept. Decide which capability the wizard needs.
- Merging main (pass 17a) into pass 17b: intake's EventLeadCreated and EventLeadAwaitingInfo
  live once, in app/legal/intake/events.go; F-CON-04's ok fixture is rehashed for the
  draft_document schema FORM-SUBJECT changed. list_unbilled now records provenance itself
  (sourcedRead) and boot also wraps every read tool (TIME-FIX); recording a value twice is
  harmless, but one of the two can go.

## Q-SHIFT-FLAG shift_date business days behind deadline.ontario_pack

- Answers the open point above: while `deadline.ontario_pack` is off, `shift_date` refuses
  `unit: business` with `ErrHolidayPackUnverified` ("holiday pack not verified"); it fails
  closed rather than counting weekends only. Calendar-day shifts are unaffected.
- `spec/eng/a2-interfaces.html` now lists 26 tools (shift_date row appended, matching the
  JSON order). The HTML row also notes the flag; the JSON description is unchanged.

## TIMEKEEPERS Active timekeepers for per-user runs

Done; resolves the TIME-FIX open item. `timekeeping.PGTimekeepers` lists active human users
(not archived, not expired in America/Toronto) whose active role grants `time.record` at any
level but `none`. `registerAgent` uses it when `AgentWiring.Timekeepers` is nil and the
kernel has a database, so F-TIM-01's end-of-day run starts per timekeeper. Unit tests stub
the query; a Postgres integration test is still to write (integration runs are on hold).

## Q-LEAD-STATUS Route that sets a lead to awaiting info

Done. Additive `api/openapi.yaml` path `POST /leads/{lead_id}/awaiting-info`
(operationId `setLeadAwaitingInfo`, Idempotency-Key, no body). `intake.StatusHTTP` checks
`intake.leads` firm-wide (403 without it), calls `Leads.SetStatus(..., awaiting_info)` and
answers 200 `{lead_id, status}`; 404 for an unknown lead, 409 `lead_closed` for a converted,
declined or sealed one. The first move into awaiting info publishes
`intake.lead_awaiting_info` (F-INT-03 starts on it). Boot mounts it with the other event
routes: `EventStores.LeadStatuses` is an `intake.Leads` on `intake.PGLeads`, publishing
through the own-transaction emitter, as lead submission does. Open: moving back to `open`
has no route yet.

## Q-EVENTS-7 Publishers for ten feature trigger events

Done; each event is appended to the `spec/eng/a2-interfaces.json` events catalogue (additive)
and has a one-outbox-event test.

- Natural write point: `consultation.booked` is published by `intake.Consultations.Book`
  (new `Events` emitter field, nil tx like the lead events). Boot does not build
  Consultations yet; set `Events` to the own-transaction emitter when it does.
- Minimal publishers (main has no write point yet; the owning task calls them when it adds
  one): `timekeeping.PublishBillDraftRequested`, `PublishTimesheetWeekSubmitted`,
  `PublishEngagementLetterSigned`, `PublishRateChangeDrafted`; `work.PublishAutomationRun`
  (failed and skipped) and `work.PublishAbsenceRequested`; `batch.PublishCycleRequested`
  (`billing.cycle_requested`); `payment.PublishBankFeedLineImported`
  (`bank_feed.line_imported`). Bodies carry ids, days, cents and reason codes only, plus
  `user_id` where the run acts for that user (trigger.PayloadActor).
- `engagement_letter.signed` stays in timekeeping, where F-TIM-05 declares it, though the
  catalogue lists module docs (the documents module owns the letter).
- `bank_feed.line_imported` is the operating bank feed (F-BIL-04 payment matching). The trust
  statement import (`POST /trust/bank-lines/imports`) does not publish it: trust lines are not
  payments to match against bills.
- Unmerged task/FEAT-BIL-01 and task/FEAT-BIL-04 declare `billing.EventBillingCycleRequested`
  and `billing.EventBankFeedLineAdded` in package billing. These publishers live in packages
  batch and payment, so nothing clashes; when those branches merge, point their constants at
  `batch.EventCycleRequested` and `payment.EventBankFeedLineImported`.


## Q-FEAT-WRK-09 F-WRK-03 triage event and the work-list screen

- Task: FEAT-WRK-09 (F-WRK-03 Triage new items). Based on `task/FEAT-WRK-08` (wrk, wrkb and
  wrkc test helpers).
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test,
  `app/legal/boot/agent.go` (the job binding and the `work-list` screen) and
  `app/legal/work/ai_wrkc_helpers_test.go`.
- No "item created untriaged" event exists. TriageBinding binds the a2 event
  `work_item.created` (reusing `docs.EventWorkItemCreated`), and the prompt tells the run to
  leave triaged items alone. Open: a dedicated untriaged event, or a filter in the job, and
  the client portal request publishing `work_item.created`.
- `work-list` selects subject kind `matter` with `work.view`. F-WRK-03 is an embedding
  feature, so no plugin skill is generated for it.


## Q-FEAT-WRK-12 F-WRK-10, F-WRK-12 and F-WRK-13 screens

- Task: FEAT-WRK-12 (read-only screen answers F-WRK-10, F-WRK-12, F-WRK-13).
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (the Defs in
  appDefs) and `app/legal/boot/agent.go` (the action screens). Built on task/FEAT-WRK-11 for
  the shared read tools and `ai_wrkd_helpers_test.go`.
- `work-item` selects subject kind `work_item` with `work.edit`, the same entry as
  FEAT-WRK-01; keep one copy when both merge.
- `workload` and `saved-filters` are firm-wide screens, but main has no `boot.FirmSubject`, so
  both select the matter with `work.view`. Switch them to the firm subject when it lands.

## Q-INTEG-18-WRK pass 18 work screens

- FEAT-WRK-12 asked for `workload` with `work.view`; main already maps `workload` to
  `work.edit` (F-WRK-07, absence cover), so the one entry keeps `work.edit` and F-WRK-12's
  "Suggest rebalance" action needs `work.edit`. Open: decide whether the read-only rebalance
  answer should need only `work.view` (that would need a per-action capability).
- FEAT-WRK-09's `ai_wrkc_helpers_test.go` additions (read-only prompt and Def checks, answer and
  outcome helpers) were folded into the shared `ai_wrk_helpers_test.go`.

## FEAT-BIL-01 F-BIL-01 billing cycle drafts

- Gap (FEAT-TOOL-08): list_unbilled does not record its figures in the run's provenance
  (get_bill and get_trust_balance wrap with pv.ReadTool; buildListUnbilled reads no
  *agenttools.Provenance). So in production a draft_bill write-down taken from list_unbilled
  is refused with ErrUnsourcedValue. The billing feature tests wrap list_unbilled with
  pv.ReadTool in their handler set (bilHandlers). Proposed: buildListUnbilled reads the
  Provenance and wraps its tool the same way.
- Manual trigger: no kernel event exists for "run the billing cycle now", so F-BIL-01 binds
  the new event type billing.cycle_requested (EventBillingCycleRequested) next to the daily
  designated-lawyer schedule. Open: which screen or route emits it.
- The Seed has no billing ports (MatterTime, UnbilledWIP, batch.Drafts, BillReader,
  BillBalances, TrustAvailable, MatterTrustAccounts), so the billing tests supply them from
  in-memory fakes ahead of the Seed's (ai_bil_helpers_test.go).

## FEAT-BIL-02 F-BIL-02 bill render and pre-flight

- draft_document's input has no cents field, so the provenance of a rendered bill's amounts
  is checked in its text: billing.SourcedBillDraftHandler wraps draft_document and refuses an
  agent draft whose title or body states an "<n> cents" figure no read returned in the run
  (ErrUnsourcedValue, before the draft is stored). A HandlerSet holds one handler per tool,
  so boot still serves the plain draft_document to every feature. Open: serve the sourced
  wrapper as the boot draft_document (it only refuses invented cents figures), or let a Def
  name per-feature handler overrides.
- F-BIL-02 binds gate.decided (billing.EventGateDecided) for the event's user; the run renders
  only when the decided gate is a G-B1 pre-bill review.

## FEAT-BIL-04 F-BIL-04 payment matching

- No kernel event exists for an imported bank feed line, so F-BIL-04 binds
  payment.received (already used by F-MAT-03) and the new bank_feed.line_imported
  (billing.EventBankFeedLineAdded). Open: which module emits the bank feed event.

## FEAT-BIL-05 Evergreen watch: amount provenance in email text and wiring

- draft_email has no cents field: the figures a retainer request states live in its subject
  and body text. billing.SourcedEmailHandlers wraps draft_email so, for an agent principal,
  every dollar ("$1,200.00") or cents ("60000 cents") figure in the text must be a value a
  read tool of the run returned, else agenttools.ErrUnsourcedValue and no draft or gate row.
  It also records list_unbilled's figures in the run's provenance (list_unbilled does not
  wrap itself with Provenance.ReadTool, unlike get_bill and get_trust_balance).
- The feature tests run over SourcedEmailHandlers(agent.Handlers()). Boot still builds one
  registry from agent.Handlers() for every feature, so the guard is not yet live at boot.
  Open: wire SourcedEmailHandlers in boot.AgentTools (it then applies to every feature's
  draft_email, e.g. F-MAT-08 cites cents from quoted engagement terms, which the guard does
  not accept), or give draft_email a per-run amount check; and wrap list_unbilled with
  ReadTool in its own file (FEAT-TOOL-08).
- The retainer-requests screen selects a matter (billing.view); main has no boot.FirmSubject.
- F-BIL-05 binds with trigger.BindDesignated (daily, the designated lawyer).

## FEAT-BIL-06 Aging and collections: gate kind const and email guard

- billing.GateCollectionsMessage names billing.collections_message (G-B7); no earlier const
  existed on main or a pushed branch. The kind is still listed in boot/unwired_gates.txt: a
  decided reminder has no effect wired yet. Open: wire its effect.
- F-BIL-06 drafts with draft_email (no submit) and opens G-B7 with submit_for_approval on the
  email_draft. It shares the sourced-amount guard of FEAT-BIL-05 (SourcedEmailHandlers), with
  the same open boot-wiring question.

## Q-INTEG-18-BIL pass 18 billing merge

- Production handlers only (AUTOPILOT policy): BIL-02's `SourcedBillDraftHandler` and BIL-05's
  `SourcedEmailHandlers` (with its `observedRead` list_unbilled wrapper and the now unused
  `EmailAmounts` parser and its test) were not wired by boot and are removed. Free-text bill
  drafts and billing emails go to the lawyer's gate; `TestFBIL02_FreeTextDraftAwaitsApproval`
  and the F-BIL-05/06 "unsourced figure awaits lawyer approval" cases now assert that
  production behaviour (draft stored, gate pending, preparer rules hold).
- `billing.EventBillingCycleRequested` and `billing.EventBankFeedLineAdded` now point at
  EVENTS-7's `batch.EventCycleRequested` and `payment.EventBankFeedLineImported`.
  `payment.received` has no EVENTS-7 publisher constant yet and stays in F-BIL-04.
- list_unbilled no longer records its own results (PROV-UNBILLED and TIME-FIX both did); boot's
  AgentTools read-tool wrapper is the one mechanism, proved for list_unbilled by
  `boot.TestAgentToolsRecordListUnbilled`. PROV-UNBILLED's build-without-provenance test went
  with the self-recording; its write-down sourcing cases moved to the boot test.
- Test helpers: `ai_bil_helpers_test.go` (package billing_test) and `ai_bilb_helpers_test.go`
  (package billing, the F-BIL-05/06 internal tests) are one file per Go package; folding them
  needs the F-BIL-05/06 tests moved to billing_test (follow-up HELPER-DEDUP).
- F-BIL-02's ok fixture prompt_hash was rehashed (draft_document's schema changed on main).

## Q-FEAT-TRU-06 F-TRU-06 schedule, screen subject and sourced worksheet amounts

## Q-FEAT-TRU-08 F-TRU-08 Draft transfer: no write tool opens trust.transfer

- features.json gives F-TRU-08 the tool `prepare_trust_withdrawal` and the gate kind
  `trust.transfer` (G11t), with an empty tool_gap. `prepare_trust_withdrawal` requests a
  withdrawal (money leaving the trust account to a payee) and opens `trust.withdrawal` (or
  `billing.trust_fee_withdrawal` with a bill id). No agent write tool requests a
  ledger-to-ledger transfer: `withdraw.Transfers` only posts an already-authorized transfer,
  and there is no transfer request API.
- Decision taken (fail closed, By-law 9): the Def declares `trust.transfer` only, as the spec
  says. In its run the production `prepare_trust_withdrawal` handler refuses every call with
  `agenttools.ErrGateKindNotAllowed` before the trust module records anything, so a transfer
  is never filed as a withdrawal. The tests prove that path, the provenance refusals
  (`ErrUnsourcedValue` for an invented or unsigned amount, via `trust.SourcedCents`) and the
  deciders of a `trust.transfer` gate on the production gate catalog.
- Open: the accept line "gate trust.transfer pending with the acting user as preparer" cannot
  be met until a transfer write tool exists. Proposed: a `prepare_trust_transfer` write tool
  (from/to matter, amount_cents sourced, reason) over a new `withdraw` transfer request that
  holds the funds and opens `trust.transfer`; add it to F-TRU-08's tools in features.json.
  This is new scope and needs owner approval.
- The `trust-transfers` screen selects matters (subject `matter`, capability
  `trust.request`): a transfer names its source and target matters.
- The end-to-end tests build every Def tool from `agent.HandlerFor`, including
  `agent.PrepareTrustWithdrawalHandler` (agenttest's driver registers its own unchecked
  wrapper of `WriteTools`, which skips the handler's gate-kind check). Helpers:
  `app/legal/trust/ai_trud_helpers_test.go` (prefix trud), shared with F-TRU-10.

## Q-FEAT-TRU-10 F-TRU-10 Propose disposition

- The run proposes one withdrawal of a residual balance through the production
  `prepare_trust_withdrawal` (refund to a matter contact, or fees for a delivered bill) and
  opens `trust.withdrawal` (G11). The tests also refuse an invented or unsigned amount
  (`ErrUnsourcedValue` via `trust.SourcedCents`), an overdraw (`withdraw.ErrTrustOverdraw`)
  and a payee that is not a matter contact, each before any withdrawal row exists.
- The `trust-balance-clearance` screen selects matters (subject `matter`, capability
  `trust.request`).
- Gap seen while testing: the production handler checks the run scope for
  `trust.withdrawal` before calling the trust module, but with a `bill_id` the module opens
  `billing.trust_fee_withdrawal` (`withdraw.RequestGateKind`). A fees disposition from
  F-TRU-10 would therefore open a kind outside the Def's scope. Proposed: the handler checks
  `withdraw.RequestGateKind` of the request it is about to make, not the fixed kind.

## Q-FEAT-TRU-11 payee-fraud-check (F-TRU-03): event, files outside the task list

- Registering F-TRU-03 in featureset.App() makes the boot tests require a job binding, so the
  task also touches app/legal/agent/featureset and app/legal/boot/agent.go.
- The a2 event list has no event for a new or changed receiving account. F-TRU-03 binds to
  `trust.receiving_account_changed` (trust.EventReceivingAccountChanged). Open: add it to
  spec/eng/a2-interfaces.json events and publish it where a payee's EFT receiving account is
  added or edited.
- The result is checked by trust.CheckPayeeFraud, which fails closed: any malformed output, an
  uncited flag, an amount no read of the run returned (ErrUnsourcedValue) or a missing
  Provenance is a block. A pass stands only with no flag; trust_overdrawn flags always lead.

## Q-FEAT-TRU-03 withdrawal-drafter (F-TRU-02): binding, screen, agenttest provenance

- Registering F-TRU-02 in featureset.App() makes the boot tests require its binding and the
  withdrawal-request screen (matter subject, trust.request), so the task also touches
  app/legal/agent/featureset, app/legal/boot/agent.go and the generated plugin skill.
- Binding: bill.delivered and matter.closed (a2 events). A client's direction has no event;
  the withdrawal-request > Draft request action starts it. Open: is "closing" the matter's
  closing (matter.closed) or a transaction closing that needs its own event?
- app/legal/agent/agenttest/feature.go (outside the task list): FromFeatureWith gave the Seed a
  Provenance of its own, so figures the production get_trust_balance and get_bill recorded
  were not the ones agenttest's real prepare_trust_withdrawal checked, and every sourced
  amount was refused. The Seed now shares the driver's Provenance, as boot wires one per run.
- agenttest's write tools read fee bills from noFeeBills, so a fee transfer to the firm's
  account always fails closed in a FromFeature run; the feature test covers it as a refusal
  only. Open: a FromFeature seam for a delivered fee bill to test the allowed fee path.
- Package trust cannot import app/legal/agent or app/legal/trust/withdraw (both reach trust),
  so trust.GateWithdrawal repeats "trust.withdrawal"; the test pins it to both constants.
- Gap seen, not changed (FEAT-TOOL-21 code): prepare_trust_withdrawal lets purpose "fees" go to
  a matter contact with no bill_id. The Postgres trust service refuses fees_to_operating
  without a bill and the authorization effect re-checks delivery, so it fails closed
  downstream, but the agent tool could refuse it earlier.

## Q-FEAT-TRU-02 bank-feed-sync write path (F-TRU-01)

- The write path is trust.ReceiptGates, the pipeline's ReceiptProposals sink: per proposed
  receipt it calls report_plan, draft_document and submit_for_approval through the run's
  registry as the agent principal, inside the run's scope (trust.receipt_classification
  only). It re-checks every receipt's amount and date against its cited bank-feed line
  before the first write (ErrUnsourcedValue), and keeps bank-feed text (the reference) out
  of the draft. Match suggestions open nothing. Open: boot wiring of the pipeline job (the
  run's matter, sources and principal) is not in this task.
- Binding: daily for the designated lawyer, plus bank_line.imported (the event POST
  /trust/bank-lines/imports declares) for "when a statement is uploaded". F-TRU-01 leaves the
  boot onRequest list for a JobBindings binding.
- Package trust cannot import app/legal/agent, so the tool names the sink calls are repeated
  as unexported constants.

## Q-TRU-FIX prepare_trust_transfer, fee-gate scope, events and the F-TRU-01 job

- Decided as counsel (trust exception): trust drafts carry figures that go to the regulator
  or to clients, so every trust feature's draft_document and draft_email runs behind the one
  trust sourced-figures check, trust.SourcedCents. It merges TRU-05's SourcedCents and
  TRU-06's SourcedDraft and keeps the stricter rule of each: signed amounts (an overdraft
  written without its minus sign is refused), dates, "*_cents" fields, thousands
  separators, and refusal of dollars, decimals and fractions of a cent. A nil Provenance
  or an empty run id fails closed.
- agent.HandlerSet resolves per (feature, tool): WithFeatureWrap adds a wrap for one
  feature, and the built tool picks per call by agent.FeatureOf(ctx). boot.AgentHandlers
  lists the wraps explicitly (trust.SourcedFeatures; no init()), and boot.ForgetRunner puts
  the run's feature in ctx. An agent call with no feature fails with agent.ErrNoFeature.
- Files outside a task list: app/legal/agent/handlerset.go, agenttest/feature.go (Fire
  scopes the feature), boot/agent.go. Non-trust drafts keep the default handler (BIL-02/05
  policy unchanged).

## Q-INTEG-18-TRUST pass 18 trust merge

- Read recording is one mechanism: `boot.AgentHandlers` wraps every read tool (recordReads)
  so it records its results in the run's Provenance, and `AgentTools` no longer wraps reads
  a second time. Tests that build on `boot.AgentHandlers()` (the trust helpers) now get read
  recording from production; the trust helper's own `ReadTool` wrap was removed. A read tool
  built without a Provenance fails with agent.ErrMissingDep.
- `ai_trub_helpers_test.go` was folded into `ai_tru_helpers_test.go` (one helper file for
  trust_test); its two checks of trust.SourcedCents moved to `ai_sourced_test.go`.
- JobBindings keeps main's designated-lawyer form (no ruleAdmin); the trust bindings plug in
  unchanged. `TRU-09` was already inside TRUST-DRAFT-WRAP's chain.

## Q-FEAT-ACC-01 Access features F-ACC-02 to F-ACC-04: shared layout

- Tasks: FEAT-ACC-01, FEAT-ACC-02, FEAT-ACC-03. Each Def joins `featureset.appDefs`, its
  featureset test row and `boot.JobBindings`; the generated plugin skill is checked in. The
  branches stack (`task/FEAT-ACC-02` on `task/FEAT-ACC-01`, `task/FEAT-ACC-03` on
  `task/FEAT-ACC-02`) so the one helper file, `app/legal/access/ai_acc_helpers_test.go`, is
  shared rather than copied.
- Firm subject: main has no `boot.FirmSubject`, and `draft_document` takes a `matter_id`
  (a2 schema). The access drafts are firm-level (licence, access review, break-glass), so the
  tests draft on a synthetic matter. Proposed follow-up: once a firm subject lands
  (`task/FORM-SUBJECT`), draft these on it.
- F-ACC-02 reuses `identity.KindUserActivation` for `core.user_activation`. Its monthly run
  binds with `trigger.BindDesignated` (fails closed without a designated lawyer); an invite run
  acts for the inviting user.
- Provenance: the access drafts carry no amount fields, and `draft_document` declares no
  provenance pointer. The tests check every date in a draft against the run's reads with
  `agenttools.Provenance` (`accSourcedDates`) and pin `ErrUnsourcedValue` for a date never read.
- F-ACC-03 declares `access.GateReviewClose` (`core.review_close`, GA8); no constant existed.
  Its quarterly run binds with `trigger.BindDesignated`. The `access-review` screen joins
  `boot.actionScreens` with subject kind `matter` (no firm subject on main) and capability
  `admin.access_review`.
- AI-014 (`ai_f_acc_03_test.go`) moved to `package access_test` and `agenttest.FromFeature`;
  its fixtures now draft with `draft_document` and submit the draft with the a2
  `submit_for_approval` input, and the ok fixture pins `match.prompt_hash`.
- F-ACC-04 declares `access.GateBreakGlassReview` (`core.break_glass_review`, GA7) and binds
  to the session end event; the run acts for the event's user, so the session's own user can
  prepare the summary but never decide it (ErrSelfApproval). AI-015 (`ai_f_acc_04_test.go`)
  moved the same way as AI-014.
- Event types with no publisher yet: `user.invited` (F-ACC-02), `break_glass.ended` (F-ACC-04;
  identity writes only the `core.break_glass_expire` audit action today).

## Q-FEAT-ACC-06 Suggest team: tool-gap gate core.matter_visibility

- core.matter_visibility is not registered by any module; F-ACC-10 opens it as a tool-gap
  kind (draft_document of the proposed team, then submit_for_approval). The matter-access
  screen already selects a matter, so no boot screen entry changed.
- search_matters returns no workload or practice-area counts, so the team's reasons come from
  the matters the run can see; a workload read tool would make them exact.
- draft_document and submit_for_approval declare no provenance fields, so no write can be
  refused with ErrUnsourcedValue at the handler; the tests check every date in the run's write
  inputs against the run's Provenance (accbSourcedDates).
- The AI-021 test (ai_f_acc_10_test.go) now runs through agenttest.FromFeature with no
  stand-ins; the fixtures use a2 inputs, the injection variant makes no extra tool call, and
  ok pins match.prompt_hash.

## Q-INTEG-18-NAMES pass 18 gate names and access helpers

- One name per gate kind: F-TRU-04's `trust.GateReconciliationSignOff` is renamed
  `trust.GateSignOff`, the name recon uses (recon imports trust, so the kind is restated and a
  test holds the two equal). `trust.GateOutgoingEmail` already has agent's name.
- `ai_acc_helpers_test.go` came from both ACC-03 (acc family) and ACC-06 (accb family); the
  two were folded into one file, each family keeping its prefix and every assertion.

## Q-FEAT-ACC-04 Offboarding plan: new event, firm-level screen, tool-gap gate

- a2 names no event for "Offboard started"; F-ACC-06 binds `user.offboard_started`
  (access.EventOffboardStarted), which no module publishes yet.
- main has no boot.FirmSubject, so the offboard-user screen action selects a matter
  (capability admin.offboard) in app/legal/boot/agent.go. It should select the leaving user
  once a firm or user subject exists.
- core.offboarding (GA12) is not registered by any module; the run opens it as a tool-gap
  kind (draft_document, then submit_for_approval). draft_document needs a matter, so the
  handover is drafted on a matter the leaver holds.
- draft_document and submit_for_approval declare no provenance fields, so no write of the
  feature can be refused with ErrUnsourcedValue at the handler. The tests check every date in
  the run's write inputs against the run's Provenance instead (accbSourcedDates).
- The AI-017 test (ai_f_acc_06_test.go) now runs through agenttest.FromFeature with no
  stand-ins; the fixtures use a2 inputs, the injection variant makes no extra tool call, and
  ok pins match.prompt_hash.

## Q-FEAT-ACC-05 Invite list check: new event, firm-level screen, row results as a draft

- a2 names no event for "list or CSV uploaded"; F-ACC-07 binds `user.invite_list_uploaded`
  (access.EventInviteListUploaded), which no module publishes yet.
- main has no boot.FirmSubject, so the user-list screen action selects a matter (capability
  admin.users) in app/legal/boot/agent.go.
- F-ACC-07 is an extraction call with tools: the row results are a draft_document draft and
  the ready rows go to gate GA2 (core.user_activation) through submit_for_approval, so the
  Def sets no OutputSchema. draft_document needs a matter, so the draft sits on a matter until
  a firm-level draft subject exists; core.user_activation's payload names one user, so a
  per-row activation proposal needs the identity module's ProposeActivation path.
- draft_document and submit_for_approval declare no provenance fields, so no write can be
  refused with ErrUnsourcedValue at the handler; the tests check every date in the run's write
  inputs against the run's Provenance (accbSourcedDates).
- The AI-018 test (ai_f_acc_07_test.go) now runs through agenttest.FromFeature with no
  stand-ins; the fixtures use a2 inputs, the injection variant makes no extra tool call, ok
  pins match.prompt_hash and bad_output still records nothing.

## Q-INTEG-18-ACC pass 18 ACC-04 and ACC-05

- FEAT-ACC-06 did not carry ACC-04 and ACC-05 (each branch was one commit on main), so pass 18
  merged task/FEAT-ACC-04 (F-ACC-06) and task/FEAT-ACC-05 (F-ACC-07) after it. Their accb
  helpers were identical to the copy ACC-06 brought, so `ai_acc_helpers_test.go` keeps one copy.


## AGENT-PROD-2a: Postgres for the six unwired agent tool ports

Production now wires Text, Documents, Entries, DraftCalc, DraftStore and BatchDrafts, so
search_documents, list_documents, the read tools, propose_time_entries and draft_bill are
served and readiness no longer reports their features `tool_unwired`. Additive changes:

- `kernel/docs.PGText` over a new append-only `docs.version_text` (0900): one row per stored
  version holding its text layer (firm document content, never conversation text). Nothing
  writes it yet except `PGText.Put`; the upload/OCR pipeline must call it, until then a
  version reads as not found and the tools fail closed.
- `app/legal/docs.FolderStore.Filed` lists a matter's live documents with a current version.
- `timekeeping.PGEntries`: a suggestion lives in `time_suggestion` (state suggested) until its
  owner accepts it; accepting writes the `time_entry` (origin suggestion) at the resolved rate
  (the default rate table, else the activity rate; none is ErrInvalid). Open: a matter's own
  rate table is read through the `PGEntries.RateTable` hook, which boot leaves nil because
  matter.Query does not expose `rate_table_id` (ARC-I-041 forbids reading matter.matter). The
  `time_entry` subject resolver also reads `time_suggestion`. Transition to on_draft_bill or
  billed is refused: the draft builder sets the bill.
- `draft.Calculator` (pure), `draft.PGStore` and `draft.PGBatch`. 5640 adds the write-down,
  markup and discount columns to `billing.bill_line` (amount_cents stays the net before the
  bill discount); 5641/5642 add `billing.bill.idempotency_key` for batch.Key (BIL-C-110).
- Open: bill lines are never deleted, so `PGStore.SaveLines` refuses a line missing from the
  saved set; draft_bill's `hold` action therefore fails closed in production. Decide whether
  hold should be passed to `PGDrafts.Create` as a line action instead.
- Open: `PGBatch` names the preparer from the agent run in ctx (`agent.run_record.acting_for`);
  a scheduled batch run with no recorded run has no preparer and fails closed.

## AGENT-PROD-3: hold, matter rate table, document text, recorded runs

Additive contract changes, each needed by the follow-ups to AGENT-PROD-2a:

- `draft.Line.Held` and `billing.bill_line.on_hold` (5643). draft_bill's `hold` is a status
  change: the line stays on the draft, held, and `PGStore.SaveLines` saves it (and can release
  it); a held line counts toward no total, tax or journal line (`PGDrafts.compute`,
  `bill.PGStore.JournalLines`). Open: the held time entry stays linked to this draft (the
  0001 `bill_line_time_once` index), so it reaches a later bill only after this draft is
  voided. `bill.PGService` still lists held lines without a held flag.
- `matter.Matter.RateTableID` (matter.rate_table_id through matter.Query). Boot sets
  `PGEntries.RateTable` from it: the matter's table prices accepted time, the default table
  only when the matter has none. Rates live on the time entry; `draft.Calculator` carries no
  rate table, so the draft takes the rate the entry was priced at.
- `kernel/docs`: `StoreText` (the upload text step), `NativeText`, `Extractor`,
  `ModelExtractor` (kernel `ai.ModelClient`, extraction call, the file as quoted tool_result
  data), `ErrTextUnavailable` (wraps ErrNotFound) and `PGText.MarkUnavailable` over the new
  `docs.version_text.unavailable_reason` (0901). Text and Markdown are stored as they are;
  other formats go through the extractor; no text is recorded as text-unavailable, never as
  empty text. Open: production has no `DocumentStore.Upload` (POST /documents is served only
  with a fake), so no production path calls `StoreText` yet; the upload, email filing and
  import paths must call it in the transaction that stores the version. The model sees the
  file only as base64 text; a vision/document-block path in `ai.Request` would be needed for
  scans.
- `boot.RecordFirstRunner`: the screen-action runner and every `AgentWiring.Runners` entry
  record the run in agent.run_record (queued, ids and grant only) before it runs, unless it
  is already recorded, so draft_bill's preparer lookup finds the run. No store or a failed
  write runs nothing. RunStarter already records first. The manual (user) draft_bill path
  still has no run and fails closed in production.

## Q-INTEG-19A pass 19a integration notes

- AGENT-PROD-3 (with 2a and AGENT-PROD) was built on a main without F-BIL-04. Its readiness
  test found propose_payment_allocation unserved: production bound the agent before the HTTP
  write ports filled Payments. `pgPayments(k)` now builds the Postgres payment service for
  both the routes and the agent port, so F-BIL-04 is ready in production.
## AGENT-PROD / AGENT-PROD-2b: production agent wiring, draft bodies, feature-run worker
- AGENT-PROD edited files outside the boot module's own list: `cmd/counseld/main.go` gained
  the model flags (`-model-mode`, `-model-base-url`, `-model-protocol`, `-model`,
  `-firm-account`; the key only from `$COUNSEL_MODEL_KEY`), and `app/legal/boot/boot.go`
  registers the agent through `w.registerAgent`, so the wiring `bindProduction` builds is the
  one registered. Both are wiring only: the flags fill `legalboot.Production`, which holds the
  logic. AGENT-PROD-2b adds the same model flags to `cmd/counsel-worker/main.go`, since the
  worker now runs the agent features. Confirm these files may be touched by agent wiring tasks.
- Draft bodies: new table `agent.draft_body` (migration 6202, block app/legal/agent) holds
  each agent draft's body sealed with AES-256-GCM under the boot seal key
  (`PGDraftBodies`). Draft rows keep only `agent.draft_body:<uuid>`. DM-020 names encrypted
  object storage; this is Postgres with app-side sealing until a bucket exists. Open: move
  the bodies to the cell bucket (kernel/docs Blobs) when one is wired, and add a reader for
  the review screens (`PGDraftBodies.Get`).
- Feature-run worker: `FeatureRunWorker` runs `agent_feature_run` jobs. The run id is a UUID
  derived from the River job id (`JobRunID`), so retries resume one run record and drafts reuse
  their rows by idempotency key; the action route now answers that run id. A transient model
  fault retries the job; an unknown feature or no acting user cancels it.
- Open: production sets no `Production.Jobs` enqueuer (the River client lives inside the
  kernel worker role), so counseld still queues no write action and no tick enqueues a
  scheduled run. The kernel needs to expose an insert-only River client to apps.
- Open: most gate kinds the agent features declare (`conflict.wall`, `docs.template_publish`,
  ...) are not registered in the boot graph; only 9 kinds are. A run that submits for one of
  those fails `gate: unknown kind`. The worker end-to-end test registers
  `docs.template_publish` itself for the scheduled case.
- AGENT-PROD-3 and AGENT-PROD-2b each added a run record store to AgentWiring: `Records`
  (boot.RunRecords, the record-first wrap of the action and job runners) and `RunRecords`
  (agent.RunRecords, the agent_feature_run worker). Both are kept; both default to
  run.PGRecords on the kernel DB. Follow-up RUN-RECORDS-ONE: fold them into one field. The
  run_record_first test helper was renamed firstRecords (it clashed with the worker's).

## ENQUEUER: kernel insert-only job client for apps

- Additive kernel change: `kboot.JobInserter`, `Kernel.Enqueuer` and `Deps.Enqueuer`. `Run`
  opens an insert-only River client (no workers, no queues) on the direct DSN (ARC-U-011)
  in both the server and the worker role, before Register, and closes it when the role
  ends. counseld only inserts; counsel-worker works the jobs on its own client.
- `Production.Jobs` stays; when nil, the agent layer enqueues on `Kernel.Enqueuer`. This
  closes the AGENT-PROD-2b open item: the screen-action route queues `agent_feature_run`,
  and the job-feature bindings are built, so the tick job (worker role) enqueues the
  scheduled runs.
- New outbox consumer `agent.feature_trigger` (`TriggerConsumer`) hands each relayed event
  to `Bindings.OnEvent`. It inserts outside the consumer's transaction; the run is unique by
  feature and event id, so a redelivery inserts nothing. A bound event naming no acting user
  is logged and skipped, not retried.
- Open: no migration creates River's tables (`river_job`, ...); nothing in the repo runs
  `rivermigrate`. The integration test migrates its own database. Decide whether
  counsel-migrate runs River's migrations or the schema goes into a numbered migration.

## RIVER-MIGRATE: River's tables come from counsel-migrate

- Closes the ENQUEUER open item. `counsel-migrate -mode=expand` now runs River's own
  migrations (`rivermigrate up`, kernel/boot `migrateRiver`) after the numbered files, then
  grants River's tables to `counsel_app` (insert) and `counsel_jobs` (work, clean, leader),
  with `river_migration` read-only. Chosen over a numbered file so River's version
  bookkeeping (`river_migration`) stays River's and a River upgrade needs no hand-copied SQL.
- Idempotent: rivermigrate skips applied versions and the grants can run again. The grant
  SQL sets `lock_timeout` 2s and `statement_timeout`; a unit test runs the migration lint on
  it. The Helm migrate Job already runs `-mode=expand`, so it gets the step; the repo has no
  compose migrate service.
- River's tables live in the migrator's current schema (`public` by default), where the
  clients resolve them. They are not recorded in `schema_meta.applied`, so `wait-version`
  does not wait for them; the expand Job runs both before it completes.
- RIVER-MIGRATE's TestProductionTriggersEnqueue predates the F-TIM-01 per-user fan-out on
  main: its tick reached the Postgres timekeepers over the fake DB. The test now wires one
  timekeeper and checks more, not less: F-TIM-01 must enqueue a run acting for that
  timekeeper, and every other scheduled run still acts for the designated lawyer.
## Q-GATE-EFFECTS approval effect for the 74 formerly unwired app gate kinds
- Every kind listed in app/legal/boot/unwired_gates.txt (now empty) gets `boot.ApprovalEffect`
  (app/legal/boot/effects_approval.go, `ApprovalKinds`), registered always, so every spec kind and
  every feature Def kind resolves in the production catalogue. The effect fails closed on an
  agent approver or the preparer (or the user an agent preparer acted for), then by subject:
  document_draft -> docs.draft status 'approved' (tool-gap kinds, e.g. intake.consult_outcome,
  timekeeping.fee_arrangement, platform.summary_accuracy); email_draft -> comm_item stamped with
  approval_id and queued for delivery (G-B7 billing.collections_message, outgoing email);
  agent_note -> matter.note confirmed_by the approver. Each statement re-matches a row the same
  gate already approved, so it is idempotent.
- Open (owner): for subjects the effect does not move (lead, conflict run, work item, deadline,
  time entry, bill, ...) the approval is recorded by the gate decision only. Kinds whose
  approval should change domain state beyond that (cal.change, trust.transfer,
  timekeeping.writedown, trust.cash_over_limit, platform.export, conflict.wall, ...) need a
  dedicated effect each; replace ApprovalEffect for the kind when one is built.
## Q-GATE-EFFECTS-CORE core.offboarding and core.matter_visibility cannot be app-registered
- Both are core.* kinds (registry.KernelPrefixes). gates.RegisterWired skips core.* and
  kernel/gate.ValidateKind rejects an app kind with a kernel prefix, and the kernel has no path
  that registers a core.* kind passed through identity.WithCoreEffect (core.trust_signatory has
  the same gap). Needs a kernel change: kernel boot registers AdminGates.Effects() kinds itself.
  Left for an owner decision; the production-catalogue test skips core.* kinds.
## Q-GATE-EFFECTS-PORTS trust.receipt_classification and billing.refund unregistered in production
- Their dedicated effects need RestPorts.Receipts (Poster) and RestPorts.Refunds (Ledger), which
  Production leaves nil, so both kinds are unregistered in production. They were never in
  unwired_gates.txt (the list is computed with test ports). Wire them in Production.
## Q-EVENTS-8 Publishers for eleven events, most with no write point yet
- Added to spec/eng/a2-interfaces.json (additive, note "EVENTS-8"): user.invited,
  user.invite_list_uploaded, user.offboard_started, break_glass.ended, email.needs_reply,
  text.received, comms.portal_message_received, trust.statement_received,
  trust.ledger_below_floor. user.role_changed and user.deactivated were already there.
- Natural write point: break_glass.ended is published by `identity.BreakGlass.Expire` in the
  expiry tx, once per grant expired (set with `BreakGlass.SetEvents`; unset publishes nothing).
- Minimal publishers, emitting in the caller's tx with id-only bodies, for write points that
  do not exist yet: `identity.UserEvents` (user.invited, user.invite_list_uploaded,
  user.offboard_started, user.role_changed, user.deactivated: there is no invite, invite-list,
  offboarding-start, user role assignment or deactivation write point in the kernel; the
  core.role_change effect changes a role, not a user's roles), `comms.MessageEvents`
  (email.needs_reply, text.received, comms.portal_message_received: no reply triage, SMS sync
  or portal message store yet) and `trust.Events` (trust.statement_received,
  trust.ledger_below_floor: no statement import or floor check yet). Each write point calls
  them when it lands; boot should set BreakGlass.SetEvents(k.Emitter) where it builds the grant
  store.
- EVENTS-8 added BreakGlass.SetEvents but no boot call. restServices now builds the
  break-glass service with withEndedEvents(..., k.Emitter), so expiry publishes
  break_glass.ended in production (TestBreakGlassPublishesEnded).
- EVENTS-8's trust/events.go redeclared EventStatementReceived and EventLedgerBelowFloor,
  which TRU-04 and TRU-05 already declare on main with the same values; the EVENTS-8 copies
  were dropped.
- New agent write tool `prepare_trust_transfer` (approved as in-spec scope, Q-FEAT-TRU-08),
  added to the a2 tools additively (spec/eng/a2-interfaces.json, schemas/tools.json; Count
  26 -> 27) and to F-TRU-08's tools in spec/ai/features.json, registered through
  agent.Handlers(). Input: from_matter_id, to_matter_id, amount_cents (sourced, integer
  cents), reason. It opens trust.transfer (G11t) only, through the run's scoped opener, with
  the acting user as preparer. Both matters must be visible to the acting user and share one
  trust account (else ErrTransferAccounts); an amount above the source ledger's available
  balance is refused (withdraw.ErrTrustOverdraw, By-Law 9). Nothing is posted or held.
- Open: the gate payload is withdraw.TransferPayload without a transfer_id. No transfer
  request store writes a trust.trust_transfer row yet, so approving the gate fails closed at
  the TransferEffect (LockTransfer finds no row) and moves no money. Proposed: a withdraw
  transfer request (row in pending_authorization, held funds) whose id the payload carries.
- prepare_trust_withdrawal's handler checks the run scope for withdraw.RequestGateKind of the
  request (billing.trust_fee_withdrawal with a bill_id), not trust.withdrawal (ARC-I-035,
  Q-FEAT-TRU-10 gap). F-TRU-02 and F-TRU-10 declare trust.withdrawal only, so a fee transfer
  with a bill is refused in their runs; F-BIL-03 declares the fee kind and passes (positive
  test over Seed.FeeBills in ai_f_tru_02_feature_test.go).
- prepare_trust_withdrawal refuses purpose=fees with no bill_id at the tool
  (agent.ErrFeesNeedBill), whoever the payee.
- Events: trust.EventBankLinesImported is now `bank_feed.line_imported`, the one name
  EVENTS-7 adds to the a2 events (not duplicated here; the a2 entry lands with EVENTS-7).
  `trust.receiving_account_changed` is added to the a2 events.
- Boot: boot.BankFeedSyncJob runs the F-TRU-01 pipeline with trust.ReceiptGates as its sink
  in the run's scope. Open: nothing works the `agent_feature_run` River job yet, so no worker
  calls it; the job-run worker should dispatch F-TRU-01 to it.
## Q-TRANSFER-STORE trust transfer row and the trust.transfer effect
- Closes the Q-TRU-FIX open item. `withdraw.TransferRequests.Request` writes the
  trust.trust_transfer row as `pending_authorization` and opens its trust.transfer gate in
  one tx (a failed open leaves no row). Under both ledger locks it refuses an amount above
  the source's `trust.available_cents` (By-Law 9). The gate payload now carries
  `transfer_id`, which `TransferEffect` binds. Recording moves no money, so it is not
  flag-gated; `Transfers.Post` at authorization still fails closed while
  `trust.bylaw9_verified` is off.
- prepare_trust_transfer reads a new agent port, `agent.TrustTransfers` (boot
  `AgentPorts.TrustTransfers`), instead of a GateOpener. It calls
  `agenttools.CheckGateKind` before the trust module is reached, as prepare_trust_withdrawal
  does. The row's `requested_by` is the acting user, and the gate preparer is the agent run
  acting for them. Open: production fill of `AgentPorts.TrustTransfers`
  (`withdraw.TransferRequests{DB, Store: withdraw.PGTransferStore{}, Gates: engine}`) lands
  with the rest of the agent port wiring. `OpenGate` is not filled in production either.
- Boot registers `withdraw.TransferEffect` for trust.transfer (removed from
  unwired_gates.txt). Approval by a signatory other than the preparer posts a transfer_out
  line and a transfer_in line, equal and opposite in integer cents, plus one balanced trust
  book entry. An overdraw at authorization is refused (insufficient_trust_funds). An agent
  decision is refused with 403 agent_cannot_decide.
- Migration 5540_trust_transfer_request.sql (additive) adds the nullable columns
  `trust_transfer.requested_by` and `trust_transfer.reason`, and freezes both with the other
  terms once the row leaves draft.
- Fee kind in F-TRU-02 and F-TRU-10: left as is. spec/ai/features.json gives both
  `gate.kind = [trust.withdrawal]` and output "draft withdrawal for G11" / "propose
  disposition"; neither spec makes a fee transfer against a bill. That is F-BIL-03 (G-B4,
  billing.trust_fee_withdrawal). F-TRU-02's trigger "bill delivered with trust applied" is
  the event after a fee transfer, not a request for one. So a prepare_trust_withdrawal call
  with a bill_id stays refused in those runs (ARC-I-035). If product wants F-TRU-02 to
  draft the fee transfer itself, add billing.trust_fee_withdrawal to its spec gate kinds
  first.
- TRANSFER-STORE (with TRU-FIX, BIL-FIX and the TRU and BIL chains): main's feature lists,
  job bindings and screens were kept and the new F-TRU-02/03/08/10 and F-BIL-03/07/08 entries
  added once each. Its recordedReads duplicated main's recordReads and was dropped; the
  F-BIL-08 credit-note wrap is kept. unwired_gates.txt stays empty (GATE-EFFECTS).
  Production now supplies AgentPorts.TrustTransfers (withdraw.TransferRequests over
  PGTransferStore, opening trust.transfer through the kernel gates), so F-TRU-08 is ready;
  with no gate opener the tool is not served. The readiness test also pins Payments and
  TrustTransfers.
